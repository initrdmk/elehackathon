package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	//	"github.com/julienschmidt/httprouter"
)

import "io/ioutil"
import "github.com/mediocregopher/radix.v2/pool"
import "github.com/mediocregopher/radix.v2/redis"
import "github.com/mediocregopher/radix.v2/pubsub"
import "database/sql"
import _ "github.com/go-sql-driver/mysql"

const debug = false

// -------------------- Global --------------------
// -------------------- Redis --------------------
var redis_pool *pool.Pool
var redis_subpub *redis.Client
var redis_sub *pubsub.SubClient
var signal chan int

func init_redis() {
	var err error
	host := os.Getenv("REDIS_HOST")
	port := os.Getenv("REDIS_PORT")

	redis_pool, err = pool.New("tcp", host+":"+port, 512)
	if err != nil {
		log.Println("init_redis")
		log.Fatal(err)
	}
	conn, err := redis_pool.Get()
	if err != nil {
		log.Println("init_redis")
		log.Fatal(err)
	}
	defer redis_pool.Put(conn)
	conn.Cmd("FLUSHDB")

	redis_subpub, err := redis.Dial("tcp", host+":"+port)
	if err != nil {
		log.Println("init_redis")
		log.Fatal(err)
	}
	redis_sub = pubsub.NewSubClient(redis_subpub)
	if redis_sub.Subscribe("food").Err != nil {
		log.Println("init_redis")
		log.Fatal(err)
	}
	if redis_sub.Subscribe("signal").Err != nil {
		log.Println("init_redis")
		log.Fatal(err)
	}
	if conn.Cmd("INCR", "SIGNAL").Err != nil {
		log.Fatal("add signal")
	}
	for {
		num, err := conn.Cmd("GET", "SIGNAL").Int()
		if err != nil {
			log.Fatal(err)
		}
		if num > 3 {
			log.Fatal("TOO MANY")
		}
		if num == 3 {
			break
		}
	}

	go func() {
		conn, err := redis_pool.Get()
		if err != nil {
			log.Println("init_redis")
			log.Fatal(err)
		}
		defer redis_pool.Put(conn)
		for {
			r := redis_sub.Receive()
			if r.Timeout() {
				continue
			}
			if r.Err != nil {
				log.Println("redis_subpub")
				log.Fatal(err)
			}
			if r.Channel == "signal" {
				signal <- 1
			} else if r.Channel == "food" {
				food_id, _ := strconv.Atoi(r.Message)
				var old *int32
				old = local_foods[food_id]
				var oldv int32
				for {
					oldv = *old
					if oldv == 0 {
						break
					}
					if !atomic.CompareAndSwapInt32(old, oldv, 0) {
						continue
					}
					if conn.Cmd("INCRBY", "f:"+strconv.Itoa(food_id), oldv).Err != nil {
						log.Fatal("food INC")
					}
				}
				conn.Cmd("PUBLISH", "signal", "go")
			}
		}
	}()

	// just try it to (useless) warm up 2333
	conn.Cmd("PUBLISH", "signal", "go")
	<-signal
	<-signal
	<-signal
}

type TokenUserCart struct {
	token   string
	user_id string
	cart_id string
}

type Cart struct {
	cart_id string
	token   string
	count   int
}

// -------------------- Login --------------------
type BodyLogin struct {
	Username string
	Password string
}

var root_uid string

func login(w http.ResponseWriter, r *http.Request) {
	// check POST
	//body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1048576))
	js, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Println("login:")
		log.Fatal(err)
	}
	if len(js) == 0 {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"EMPTY_REQUEST","message":"请求体为空"}`))
		return
	}
	var body BodyLogin
	//if err := r.Body.Close(); err != nil { }
	if err := json.Unmarshal(js, &body); err != nil {
		if debug {
			log.Println("login: 400")
		}
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"MALFORMED_JSON","message":"格式错误"}`))
		return
	}

	// You will never know the (althrough pre-)generated token unless you login
	user, ok := userps[body.Username]
	//log.Printf("uname: %s; upass: %s; uupass: %s\n", body.Username, user.password, body.Password)
	if !ok || user.password != body.Password {
		if debug {
			log.Println("login: 403")
		}
		w.WriteHeader(403)
		w.Write([]byte(`{"code":"USER_AUTH_FAIL","message":"用户名或密码错误"}`))
		return
	}

	//log.Println("==== ")
	w.WriteHeader(200)
	//log.Printf(`{"user_id":%s,"username":"%s","access_token":"%s"}`,
	//	user.user_id, body.Username, token)
	w.Write([]byte(fmt.Sprintf(`{"user_id":%s,"username":"%s","access_token":"%s"}`,
		user.user_id, body.Username, user.token)))
}

// -------------------- Cart --------------------
type BodyCart struct {
	Food_id int
	Count   int
}
type BodyCartId struct {
	Cart_id string
}

func carts(w http.ResponseWriter, r *http.Request) {
	token, err := get_token(w, r)
	if err != nil {
		if debug {
			log.Println("error in carts")
		}
		return
	}
	if r.Method == "POST" {
		post_carts(w, token)
	} else if r.Method == "PATCH" {
		js, _ := ioutil.ReadAll(r.Body)
		if len(js) == 0 {
			w.WriteHeader(400)
			w.Write([]byte(`{"code":"EMPTY_REQUEST","message":"请求体为空"}`))
			return
		}
		var body BodyCart
		//if err := r.Body.Close(); err != nil { }
		if err := json.Unmarshal(js, &body); err != nil {
			w.WriteHeader(400)
			w.Write([]byte(`{"code":"MALFORMED_JSON","message":"格式错误"}`))
			return
		}
		suffix := strings.TrimPrefix(r.RequestURI, "/carts/")
		cart_id := strings.Split(suffix, "?")[0]

		patch_carts(w, token, cart_id, body)

	} else {
		println("WRONG ME")
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
}

func post_carts_slow_path(w http.ResponseWriter, token string) {
	conn, err := redis_pool.Get()
	if err != nil {
		log.Println("post_cart")
		log.Fatal(err)
	}
	defer redis_pool.Put(conn)

	user_id, ok := token2uid[token]
	if !ok {
		if debug {
			log.Println(err)
		}
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	var cart_id string
	for {
		cart_id = user_id + raw_next_rand_less()
		r, err := conn.Cmd("SETNX", "c:"+cart_id, user_id).Int()
		if err != nil {
			log.Fatal(err)
		}
		if r == 1 {
			cart2token_ext_rwmutex.Lock()
			cart2token_ext[cart_id] = token
			cart2token_ext_rwmutex.Unlock()
			break
		}
	}
	w.Write([]byte(fmt.Sprintf(`{"cart_id":"%s"}`, cart_id)))
}
func post_carts(w http.ResponseWriter, token string) {
	carts, ok := token2cart[token]
	if !ok {
		if debug {
			log.Println("invalid token")
		}
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	if carts.used {
		post_carts_slow_path(w, token)
	}

	carts.used = true
	w.Write([]byte(fmt.Sprintf(`{"cart_id":"%s"}`, carts.cart_ids[0])))
}

func patch_carts(
	w http.ResponseWriter,
	token string,
	cart_id string,
	body BodyCart) {

	conn, err := redis_pool.Get()
	if err != nil {
		log.Println("patch_cart")
		log.Fatal(err)
	}
	defer redis_pool.Put(conn)

	if err != nil {
		log.Println("patch_cart")
		log.Fatal(err)
	}
	_, ok := token2uid[token]
	if !ok {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}

	var cart_token string
	cart_token, ok = cart2token[cart_id]
	if !ok {
		cart2token_ext_rwmutex.RLock()
		cart_token, ok = cart2token_ext[cart_id]
		cart2token_ext_rwmutex.RUnlock()
		if !ok {
			w.WriteHeader(404)
			w.Write([]byte(`{"code":"CART_NOT_FOUND","message":"篮子不存在"}`))
			return
		}
	}

	if cart_token != token {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"NOT_AUTHORIZED_TO_ACCESS_CART","message":"无权限访问指定的篮子" }`))
		return
	}

	_, ok = foods_cache[body.Food_id]
	if !ok {
		w.WriteHeader(404)
		w.Write([]byte(`{"code":"FOOD_NOT_FOUND","message":"食物不存在"}`))
		return
	}

	// TODO: potential speed up
	//  Spawn a go routine to do the redis at the very beginning of this func.
	//fmt.Printf("%+v\n", cart)
	for i := 0; i < body.Count; i++ {
		conn.PipeAppend("LPUSHX", "c:"+cart_id, body.Food_id)
	}
	tot_count := 0
	for i := 0; i < body.Count; i++ {
		tot_count, err = conn.PipeResp().Int()
		if err != nil {
			log.Fatal(err)
		}
	}
	if tot_count > 3 {
		w.WriteHeader(403)
		w.Write([]byte(`{"code":"FOOD_OUT_OF_LIMIT","message":"篮子中食物数量超过了三个"}`))
		return
	}

	w.WriteHeader(204)
	w.Write([]byte(""))
}

// -------------------- Order --------------------
type BodyOrder struct {
	Cart_id string
}

func post_orders(w http.ResponseWriter, token string, body BodyOrder) {

	conn, err := redis_pool.Get()
	if err != nil {
		log.Println("post_orders")
		log.Fatal(err)
	}
	defer redis_pool.Put(conn)

	cart_id := body.Cart_id
	user_id, ok := token2uid[token]
	if !ok {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	cart_token, ok := cart2token[cart_id]
	if !ok {
		cart2token_ext_rwmutex.RLock()
		cart_token, ok = cart2token_ext[cart_id]
		cart2token_ext_rwmutex.RUnlock()
		if !ok {
			w.WriteHeader(404)
			w.Write([]byte(`{"code":"CART_NOT_FOUND","message":"篮子不存在"}`))
			return
		}
	}
	if cart_token != token {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"NOT_AUTHORIZED_TO_ACCESS_CART","message":"无权限访问指定的篮子" }`))
		return
	}

	cart, err := conn.Cmd("GET", "c:"+cart_id).List()
	if err != nil {
		log.Fatal(err)
	}

	// ==================== commit ====================
	order := fmt.Sprintf(`{"id":"%s","user_id":%s,"items":[`, token, user_id)
	total := 0
	null := true
	//conn.PipeAppend("MULTI")
	//fmt.Printf("%+v\n", cart)
	ll := len(cart)
	food_ids := make([]int, ll)
	counts := make([]int32, ll)
	local_done := make([]bool, ll)
	for i, food_str := range cart {
		ifood_id, _ := strconv.Atoi(food_str)
		food_ids[i] = ifood_id
		counts[i] = 1
		local_done[i] = false
	}
	for i := 0; i < ll; i++ {
		for j := i + 1; j < ll; j++ {
			if food_ids[i] == food_ids[j] {
				counts[j] += counts[i]
				counts[i] = 0
			}
		}
	}

	for i, food_id := range food_ids {
		if counts[i] == 0 {
			continue
		}
		if !null {
			order += ","
		}
		order += fmt.Sprintf(`{"food_id":%d,"count":%d}`, food_id, counts[i])
		total += foods_cache[food_id].price * int(counts[i])
		null = false
	}
	order += fmt.Sprintf(`],"total":%d}`, total)

	need_slow_path := false
	for i, food_id := range food_ids {
		if counts[i] == 0 {
			continue
		}
		var old *int32 = local_foods[food_id]
		var oldv int32 = *old
		for {
			oldv = *old
			if oldv == -1 {
				need_slow_path = true
				break
			}
			if oldv < counts[i] {
				need_slow_path = true
				conn.Cmd("PUBLISH", "food", food_id)
				<-signal
				<-signal
				<-signal
			}
			// local storage is sufficient
			if !atomic.CompareAndSwapInt32(old, oldv, oldv-counts[i]) {
				continue
			}
			// local storage is acquired successfully
			local_done[i] = true
		}
	}
	if !need_slow_path {
		r, err := conn.Cmd("SETNX", "o:"+token, order).Int()
		if err != nil {
			log.Fatal("Err in fast path")
		}
		if r == 0 {
			// delay until here for fast path
			w.WriteHeader(403)
			w.Write([]byte(`{"code":"ORDER_OUT_OF_LIMIT","message":"每个用户只能下一单"}`))
			return
		}
		// FIXME
		w.Write([]byte(fmt.Sprintf(`{"id":"%s"}`, token)))
		return
	}

	// slow_path
	for i, food_str := range cart {
		if counts[i] == 0 {
			continue
		}
		if local_done[i] {
			continue
		}
		conn.PipeAppend("DECRBY", "f:"+food_str, counts[i])
	}
	conn.PipeAppend("SETNX", "o:"+token, order)
	succ := true
	for i, _ := range cart {
		if counts[i] == 0 {
			continue
		}
		if local_done[i] {
			continue
		}
		c, _ := conn.PipeResp().Int()
		if c < 0 {
			succ = false
		}
	}
	r, _ := conn.PipeResp().Int()
	setok := r == 1
	if !setok {
		succ = false
	}
	if !succ {
		// async roll back
		go func(cart []string, counts []int32, token string, del bool) {
			conn, err := redis_pool.Get()
			if err != nil {
				log.Println("post_orders")
				log.Fatal(err)
			}
			defer redis_pool.Put(conn)

			for i, food_str := range cart {
				if counts[i] == 0 {
					continue
				}
				conn.PipeAppend("INCBY", "f:"+food_str, counts[i])
			}
			if del {
				conn.PipeAppend("DEL", "o:"+token)
			}
			for i, _ := range cart {
				if counts[i] == 0 {
					continue
				}
				conn.PipeResp()
			}
			if del {
				conn.PipeResp()
			}
		}(cart, counts, token, setok)

		if !setok {
			w.WriteHeader(403)
			w.Write([]byte(`{"code":"ORDER_OUT_OF_LIMIT","message":"每个用户只能下一单"}`))
			return
		}
		w.WriteHeader(403)
		//println("insucc")
		w.Write([]byte(`{"code": "FOOD_OUT_OF_STOCK","message": "食物库存不足"}`))
		return
	}
	w.Write([]byte(fmt.Sprintf(`{"id":"%s"}`, token)))
	return
}

func get_orders(w http.ResponseWriter, token string) {
	conn, err := redis_pool.Get()
	if err != nil {
		log.Println("get_orders")
		log.Fatal(err)
	}
	defer redis_pool.Put(conn)
	r, err := conn.Cmd("GET", "o:"+token).Str()
	if err != nil || r == "" {
		//log.Fatal(err)
		w.Write([]byte("[]"))
	} else {
		w.Write([]byte("[" + r + "]"))
	}
	return
}

func orders(w http.ResponseWriter, r *http.Request) {
	token, err := get_token(w, r)
	if err != nil {
		return
	}
	if r.Method == "GET" {
		get_orders(w, token)
	} else if r.Method == "POST" {
		js, _ := ioutil.ReadAll(r.Body)
		if len(js) == 0 {
			w.WriteHeader(400)
			w.Write([]byte(`{"code":"EMPTY_REQUEST","message":"请求体为空"}`))
			return
		}
		var body BodyOrder
		//if err := r.Body.Close(); err != nil { }
		if err := json.Unmarshal(js, &body); err != nil {
			w.WriteHeader(400)
			w.Write([]byte(`{"code":"MALFORMED_JSON","message":"格式错误"}`))
			return
		}
		post_orders(w, token, body)
	} else {
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
}

func admin_orders(w http.ResponseWriter, r *http.Request) {
	token, err := get_token(w, r)
	if err != nil {
		return
	}
	conn, err := redis_pool.Get()
	if err != nil {
		log.Println("admin_orders")
		log.Fatal(err)
	}
	defer redis_pool.Put(conn)

	user_id, err := conn.Cmd("GET", "t:"+token).Str()
	if err != nil || user_id == "" {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	if user_id != root_uid {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}

	keys, _ := conn.Cmd("KEYS", "o:*").List()
	if len(keys) == 0 {
		w.Write([]byte("[]"))
		return
	}
	values, _ := conn.Cmd("MGET", keys).List()
	null := true
	ret := `[`
	for _, v := range values {
		if !null {
			ret += ","
		}
		ret += v
		null = false
	}
	ret += `]`
	w.Write([]byte(ret))
}

var token2uid map[string]string

type Carts struct {
	// [0] is mine
	cart_ids [3]string
	used     bool
}

var token2cart map[string]*Carts
var cart2token map[string]string

var cart2token_ext_rwmutex sync.RWMutex
var cart2token_ext map[string]string

// -------------------- Food --------------------
type Food struct {
	//	food_id int
	price int
	stock int
}

var done_orders map[string]string
var foods_cache map[int]Food
var local_foods map[int]*int32
var sorted_foods_keys []int

// TODO: weaken the consistency
func foods(w http.ResponseWriter, r *http.Request) {
	// check GET
	_, err := get_token(w, r)
	if err != nil {
		return
	}
	null := true
	ret := `[`
	for _, id := range sorted_foods_keys {
		food := foods_cache[id]
		if !null {
			ret += fmt.Sprintf(`,{"id":%d,"price":%d,"stock":%d}`, id, food.price, food.stock)
		} else {
			ret += fmt.Sprintf(`{"id":%d,"price":%d,"stock":%d}`, id, food.price, food.stock)
		}
		null = false
	}
	ret += `]`

	w.Write([]byte(ret))
}

// TODO: make things async
//  e.g. use goroutine to calc `total`
type User struct {
	user_id  int
	username string
	// access_token string // hidden to map key
	done  bool
	total int
	//order Cart // get async
}

type UserP struct {
	user_id  string
	password string
	token    string
}

const TOKEN_COUNT = 301001
const TOKEN_LEN = 24

var tokens [TOKEN_COUNT]string
var userps map[string]*UserP
var token2order map[string]string

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

var randp int64

func gen_token() {
	for i := 0; i < TOKEN_COUNT; i++ {
		tokens[i] = RandStringBytes(TOKEN_LEN)
	}
	randp = 0
}
func raw_next_rand_less() string {
	if TOKEN_LEN < 0 {
		return RandStringBytes(TOKEN_LEN)
	}
	return RandStringBytes(TOKEN_LEN - 4)
}

func raw_next_rand() string {
	return RandStringBytes(TOKEN_LEN)
}
func nextRand() string {
	got := atomic.AddInt64(&randp, 1)
	if got >= TOKEN_COUNT {
		return RandStringBytes(TOKEN_LEN)
	}
	return tokens[got]
}

// -------------------- MySql --------------------
var db *sql.DB

func cache_foods() {
	var id int
	var price int
	var stock int
	foods_cache = map[int]Food{}
	local_foods = map[int]*int32{}
	rows, err := db.Query("SELECT id, stock, price FROM food")
	if err != nil {
		log.Println("cache_foods")
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		err := rows.Scan(&id, &price, &stock)
		if err != nil {
			log.Println("cache_foods")
			log.Fatal(err)
		}
		foods_cache[id] = Food{stock, price}
	}
	err = rows.Err()
	if err != nil {
		log.Println("cache_foods")
		log.Fatal(err)
	}

	sorted_foods_keys = make([]int, 0)
	for k, _ := range foods_cache {
		sorted_foods_keys = append(sorted_foods_keys, k)
	}
	sort.Ints(sorted_foods_keys)

	conn, err := redis_pool.Get()
	if err != nil {
		log.Println("cache_foods")
		log.Fatal(err)
	}
	defer redis_pool.Put(conn)
	for k, v := range foods_cache {
		conn.Cmd("SETNX", "f:"+strconv.Itoa(k), v.stock)
	}
	for k, v := range foods_cache {
		var p int32 = int32(v.stock) / 3
		local_foods[k] = &p
		conn.Cmd("DECRBY", "f:"+strconv.Itoa(k), local_foods[k])
	}
	log.Println("food cached")

	conn.Cmd("PUBLISH", "signal", "go")
	<-signal
	<-signal
	<-signal
	log.Println("all food cached")
}
func cache_users() {
	var id int
	var name string
	var pass string
	userps = map[string]*UserP{}

	rows, err := db.Query("SELECT id, name, password FROM user")
	if err != nil {
		log.Println("cache_users")
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id, &name, &pass)
		if err != nil {
			log.Println("cache_users")
			log.Fatal(err)
		}
		userps[name] = &UserP{strconv.Itoa(id), pass, ""}
		if name == "root" {
			root_uid = strconv.Itoa(id)
		}
	}
	err = rows.Err()
	if err != nil {
		log.Println("cache_users")
		log.Fatal(err)
	}
	log.Println("user cached")

	conn, err := redis_pool.Get()
	if err != nil {
		log.Println("cache_users")
		log.Fatal(err)
	}
	defer redis_pool.Put(conn)
	for _, v := range userps {
		token := "t=" + raw_next_rand()
		if conn.Cmd("SETNX", "u:"+v.user_id, token).Err != nil {
			log.Fatal(err)
		}
	}
	log.Println("user warmed")
	for k, v := range userps {
		token, err := conn.Cmd("GET", "u:"+v.user_id).Str()
		if err != nil {
			log.Fatal(err)
		}
		if conn.Cmd("SETNX", "t:"+token, v.user_id).Err != nil {
			log.Fatal(err)
		}
		userps[k].token = token
		token2uid[token] = v.user_id
		token2cart[token] = &Carts{[3]string{"", "", ""}, false}
		for {
			cart_id := v.user_id + raw_next_rand_less()
			r, err := conn.Cmd("SETNX", "c:"+cart_id, v.user_id).Int()
			if err != nil {
				log.Fatal(err)
			}
			if r == 1 {
				// set mine
				token2cart[token].cart_ids[0] = cart_id
				break
			}
		}
	}
	log.Println("cart warmed")
	conn.Cmd("PUBLISH", "signal", "go")
	<-signal
	<-signal
	<-signal
	log.Println("all cart warmed")
	for _, v := range userps {
		var carts []string
		var err error
		token := v.token
		carts, err = conn.Cmd("KEYS", "c:"+v.user_id).List()
		if err != nil {
			log.Fatal(err)
		}
		if len(carts) < 3 {
			log.Fatal("too small")
			break
		}
		for _, carts := range carts {
			cart2token[carts] = token
			for i, _ := range token2cart[token].cart_ids {
				if token2cart[token].cart_ids[i] == carts {
					break
				}
				if token2cart[token].cart_ids[i] != "" {
					continue
				}
				token2cart[token].cart_ids[i] = carts
			}
		}
	}
	log.Println("cart warmed")
	conn.Cmd("PUBLISH", "signal", "go")
	<-signal
	<-signal
	<-signal
	log.Println("all cart warmed")
}

func init_mysql() {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	name := os.Getenv("DB_NAME")
	user := os.Getenv("DB_USER")
	pass := os.Getenv("DB_PASS")
	var err error
	db, err = sql.Open("mysql",
		user+":"+pass+"@tcp("+
			host+":"+port+")/"+name)
	if err != nil {
		panic(err.Error())
	}
	//defer db.Close()
	err = db.Ping()
	if err != nil {
		panic(err.Error())
	}
}

// -------------------- main --------------------
func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	host := os.Getenv("APP_HOST")
	port := os.Getenv("APP_PORT")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "8080"
	}
	addr := fmt.Sprintf("%s:%s", host, port)

	f, _ := os.Open("/dev/urandom")
	b := make([]byte, 8)
	f.Read(b)
	f.Close()
	var seed int64 = 0
	for _, v := range b {
		seed <<= 8
		seed |= int64(v)
	}
	rand.Seed(seed)

	gen_token()
	init_mysql()
	init_redis()
	cache_users()
	cache_foods()

	http.HandleFunc("/login", login)
	http.HandleFunc("/foods", foods)
	http.HandleFunc("/carts", carts)
	http.HandleFunc("/carts/:cart_id", carts)
	http.HandleFunc("/", carts)
	http.HandleFunc("/orders", orders)
	http.HandleFunc("/admin/orders", admin_orders)

	log.Println("serving...")
	http.ListenAndServe(addr, nil)
}

func get_token(w http.ResponseWriter, r *http.Request) (string, error) {
	token := r.URL.Query().Get("access_token")
	if token != "" {
		return token, nil
	}
	token = r.Header.Get("Access-Token")
	if token != "" {
		return token, nil
	}
	w.WriteHeader(401)
	w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
	return "", errors.New("")
}

/*
save in memory:

map [foodid] Food
Food {
	price int
	count int
}

map [username] password
map [token] User
map [token] cartid

User {
	user_id int
	username string
	access_token string
	done bool
	total int
	order DoneOrder { // calc async
	} => string
}

type Item {
	food_id int
	count int
}

type Cart struct {
	cart_id string
	items [3]Item
	access_token string
}


// TODO: make things async
//  e.g. use goroutine to calc `total`
type User struct {
	user_id int
	username string
	// access_token string // hidden to map key
	done bool
	total int
	order Cart // get async
}

save in redis:
access_token => orders (string)
food_id => count (int)

*/
