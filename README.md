# Go Code for Eleme Hackathon

废话懒得说，直接上干货吧。

## Summary

总的设计原则是: common case fast, rare case correct。尽量在本地缓存，
对频繁出现的情况（benchmark中的请求）要尽可能的快，对corner case要保证不出错（test中的请求）。

由于好多人说网络IO是大头，所以对这边做了最多的优化，最后结果如下：


| method         | 一般情况下（最优情况）的网络请求数 |        最差情况的网络请求数        |
|----------------|:----------------------------------:|:----------------------------------:|
| /login         |                  0                 |                  0                 |
| /cart          |                  0                 |                  1                 |
| /cart/:cart_id |                  1                 |           1+(#food) sync           |
| /order         |          1 sync + 1 async          | (2+2*#food) sync + (1+#food) async |
| /food          |                  0                 |                  0                 |


- \#food 在/cart/:cart_id中是这次请求要添加的food的数量
- \#food 在/order中是想要提交的篮子中的food的数量


## All Tricks

这里讲了一些用到的主要的trick，这些都是为了最大化性能。*但是*并不会影响程序的正确性，
也就是说在product environment中也可以采用类似策略的。这不仅仅只能用于比赛。

- 所有的user token都预先生成好，然后用SETNX放到redis上去，之后再从redis上拉下来。
这样不管有多少台机器，他们得到的token都是一致的。这部分是预处理的时候做的。

```
      // pre-process
      for uid in users {
         usertoken = generate_user_token();
         redis.setnx(uid, usertoken)
      }
      for uid in users {
         usertoken = redis.get(uid)
         local_user[usertoken] = uid
         local_token[uid] = token
      }
      // when /login
      if is_password_ok() {
         token = local_token[uid]
         reply_token_to_user()
      }
```

   需要验证token的时候

```
      // when validing the tokens
      uid = local_user[token]
      if uid == nil {
         invalid_token()
         return
      }
```

这样在处理`/login`的时候，只需要在本地的map里面查找做验证就好了。

- 每台机器在本地为每个用户生成一个cart_id，然后把cart_id放到redis上去，
这样如果有三台机器，那么每个用户就会被预先生成好3个cart_id，这三个都是可以用的。
验证的时候只要看是不是这三个之一就好。

```
   // pre-process
   for uid in users {
      cart_id = generate_cart_id()
      redis.listadd(uid, cart_id)
      cart_id_generated_by_me[uid] = cart_id
   }
   // wait for all machines done
   barrier()
   for uid in users {
      all_cart_ids = redis.listget(uid)
      for cart_id in all_cart_ids {
         // all these cart_ids are valid
         local_cart_id[cart_id] = uid
      }
   }
```

在处理`POST /cart`的时候，找到本机器生成的cart_id，然后返回。这里也没有网络操作。

```
   // POST /cart
   uid = valid_token_and_get_uid(token)
   if cart_id_generated_by_me_is_used[uid] {
      // slow path
      // generate a new cart_id
      // too simple to show here
   } else {
      cart_id_generated_by_me_is_used[uid] = true
      // return cart_id_generated_by_me[uid]
      reply_cart_id_generated_by_me()
   }
```

在需要验证cart_id的时候：

```
   // when validing the cart_id
   uid = local_cart_id[cart_id]
   if uid == nil {
      invalid_cart_id()
      return
   }
```

- 在添加食物的时候，老老实实添加食物就好了，但是可以利用redis的list操作会返回list里元素的个数，检查是不是超过3个food。

```
   // PATCH /cart/:cart_id
   uid = valid_token_and_get_uid(token)
   valid_cart_id(cart_id)
   valid_food_id(food_id)
   loop food_count times {
      num = redis.listadd(cart_id, food_id)
      if num > 3 {
         reply_too_much()
         return
      }
   }
   reply_ok()
```

需要一次listadd操作。

- 重头戏是提交订单`POST /order`。
这里也是一个比较大的trick。想想京东不会把所有的存货都放到北京的仓库里面吧？对的，每台机器先从redis里面拿一定量的food出来（这里是333个），
然后如果本地货源充足，就不用走redis了。(大多数情况是不走redis的，所以大多数情况是只有一次同步的网络请求）
如果本地的存货不够了，那么就通知所有机器把当前这种food全部退回到redis中，当所有人都退回之后，
之后的每次操作都从redis中读取。当然读取的时候也要按照optimistic的方法，即先去减一，如果发现小于0了，直接abort订单，并且异步的把之前减一的再加回去。

```
   // pre-process
   for food_id in foods {
      tot_amount = foods_amount_from_mysql[food_id]
      redis.setnx(food_id, tot_amount)
      local_food_amount[food_id] = tot_amount / machine_number
      redis.decrby(food_id, local_food_amount[food_id])
   }

   // when POST /order
   uid = valid_token_and_get_uid(token)
   valid_cart_id(cart_id)
   cart = redis.listget(cart_id)
   for i, foor_id := carts {
      // use CAS operations to protect this when implementing
      int result = --local_food_amount[food_id]
      if result > 0 {
         local_served[i] = true
      } else if result == 0 {
         local_served[i] = true
         notify_other_machines_to_return_food(food_id)
      } else {
         local_served[i] = false
         notify_other_machines_to_return_food(food_id)
         // this will block
         wait_for_other_machines_to_return_food(food_id)
      }
   }
   if all_true(local_served) {
      // do nothing
   } else {
      for_those_food_id_not_local_served {
         result = redis.decr(food_id)
         if result < 0 {
            reply_food_not_enough
            go func() {
               for_food_is_that_has_been_decreased {
                  redis.incr(food_id)
               }
            }
            return
         }
      }
   }
   succ = redis.setnx(token_id_as_order_id, the_order_string)
   if !succ {
      reply_one_user_cannot_order_twice
      return
   }
   reply_ok
```

- 订单查询还是很方便的，测试中对于性能也没太大要求。可以做本地缓存。
- food的个数的查询，采用异步的方式，每个机器没隔一段时间，就把自己本地剩余的数量放到redis上去，
然后redis拿到所有机器中剩余food的数量和在redis存放的数量，加在一起，更新机器本地的缓存就好。


最后有几点提一下，token和cart_id的预先生成不会产生很严重的安全问题。至少和猜对密码是同等level的。
food的异步统计也不会产生很大的问题，毕竟高并发情况下很难说究竟剩下多少food。

## The biggest problem

最大的问题在于，要知道有多少个机器，才能够使用go的channel和redis的pubsub进行机器之间的同步。

这里使用的一种比较傻的办法，每个机器在启动的时候`redis.incr("SIGNAL")`，然后等待5s，
最后再`redis.get("SIGNAL")`，这样如果没出太大问题，就可以知道运行的机器的个数了。

barrier()的实现，就是等所有机器都发过来一个signal的publish。
wait_for_other_machines_to_return_food也是类似的方式。只要把go的channel和redis的pubsub结合在一起就好了。


## 最最后的教训

Seeing is believing. Never trust anyone indiscriminately, since he is not in your shoes.

Good luck.

MK
Dec. 4th, 2015
