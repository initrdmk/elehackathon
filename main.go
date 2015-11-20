package main

import (
	"fmt"
	"net/http"
	"os"
	"encoding/json"
	"log"
	"math/rand"
)

import "io/ioutil"
import "github.com/mediocregopher/radix.v2/pool"
import "database/sql"
import _ "github.com/go-sql-driver/mysql"

// -------------------- Redis --------------------
var redis *pool.Pool
func init_redis() {
	redis, err := pool.New("tcp", "localhost:6379", 10)
	if err != nil {
		log.Fatal(err)
	}
}

func redis_add_token(token string, uid int) {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	if conn.Cmd("PUT", token, uid).Err != nil {
		log.Fatal(err)
	}
	redis.Put(conn)
}


func redis_set(key string, value string) {

}

func redis_get(key string) {

}


const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}


// -------------------- Login --------------------
type BodyLogin struct {
	username string
	password string
}
func login(w http.ResponseWriter, r *http.Request) {
	// check POST
	//body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1048576))
	js, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(400)
		w.Write([]byte(`{
			"code": "EMPTY_REQUEST",
			"message": "请求体为空"
		}`))
		return
	}
	var body BodyLogin;
	//if err := r.Body.Close(); err != nil { }
	if err := json.Unmarshal(js, &body); err != nil {
		w.WriteHeader(400)
		w.Write([]byte(`{
			"code": "MALFORMED_JSON",
			"message": "格式错误"
		}`))
		return
	}

	user, ok := userps[body.username]
	if !ok || user.password != body.password {
		w.WriteHeader(403)
		w.Write([]byte(`{
			"code": "USER_AUTH_FAIL",
			"message": "用户名或密码错误"
		}`))
		return
	}
	token := "token" + RandStringBytes(10)
	// TODO: multi redis instance, each for one table?
	redis_add_token(token, user.user_id)
	w.Write([]byte(fmt.Sprintf(`{
		"user_id": %d,
		"username": %s,
		"access_token": %s
	}`, user.user_id, body.username, token)))
}

// -------------------- Cart --------------------
type BodyCart struct {
	food_id int
	count int
}
func carts(w http.ResponseWriter, r *http.Request) {
	token, err := get_token(w, r)
	if err != nil {
		return
	}
	if r.Method == "POST" {
		post_carts(w, token)
	} else if r.Method == "PATCH" {
		js, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte(`{
				"code": "EMPTY_REQUEST",
				"message": "请求体为空"
			}`))
			return
		}
		var body BodyCart;
		//if err := r.Body.Close(); err != nil { }
		if err := json.Unmarshal(js, &body); err != nil {
			w.WriteHeader(400)
			w.Write([]byte(`{
				"code": "MALFORMED_JSON",
				"message": "格式错误"
			}`))
			return
		}
		patch_carts(w, token, cart_id, body)
	} else {
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
}

func post_carts(w http.ResponseWriter, token string) {
	cart_id = "cart1d" + RandStringBytes(11)
	redis_add_cart(card_id, token)
	w.Write([]byte(fmt.Sprintf(`{
		"cart_id": %d
	}`, cart_id)))
}

func patch_carts(
	w http.ResponseWriter,
	token string,
	cart_id int,
	body BodyCart) {

	cart = redis_get_cart(cart_id)
	if cart == nil {
		w.Write([]byte(`{
			"code": "CART_NOT_FOUND",
			"message": "篮子不存在"
		}`))
		return
	}
	if token != cart.token {
		w.WriteHeader(401)
		w.Write([]byte(`{
			"code": "NOT_AUTHORIZED_TO_ACCESS_CART",
			"message": "无权限访问指定的篮子"
		}`))
		return
	}
	if cart.count >= 3 {
		w.WriteHeader(403)
		w.Write([]byte(`{
			"code": "FOOD_OUT_OF_LIMIT",
			"message": "篮子中食物数量超过了三个"
		}`))
		return
	}

	_, ok := foods_cache[food_id]
	if !ok {
		w.WriteHeader(404)
		w.Write([]byte(`{
			"code": "FOOD_NOT_FOUND",
			"message": "食物不存在"
		}`))
		return
	}
	redis_add_to_cart(cart_id, food_id, count)
	w.WriteHeader(204)
}

// -------------------- Order --------------------
type BodyOrder struct {
	cart_id string
}
func post_orders(w http.ResponseWriter, token string, body BodyOrder) {
	cart = redis_get_cart(body.cart_id)
	if cart == nil {
		w.WriteHeader(404)
		w.Write([]byte(`{
			"code": "CART_NOT_FOUND",
			"message": "篮子不存在"
		}`))
		return
	}
	if token != cart.token {
		w.WriteHeader(403)
		w.Write([]byte(`{
			"code": "NOT_AUTHORIZED_TO_ACCESS_CART",
			"message": "无权限访问指定的篮子"
		}`))
		return
	}
	if token.done {
		w.WriteHeader(403)
		w.Write([]byte(`{
			"code": "ORDER_OUT_OF_LIMIT",
			"message": "每个用户只能下一单"
		}`))
	}
	err := redis_commmit_order(cart)
	if  err != nil {
		w.WriteHeader(403)
		w.Write([]byte(`{
			"code": "FOOD_OUT_OF_STOCK",
			"message": "食物库存不足"
		}`))
		return
	}

	w.Write([]byte(`{
		"id ": "0"
	}`))
}

func get_orders(w http.ResponseWriter, token string) {
	order, ok := token2order[token];
	if ok {
		w.Write(order)
		return
	}
	order, ok = redis_get("done_order" + token)
	if ok {
		token2order[token] = order
		w.Write(order)
		return
	}
	w.Write("")
}

func orders(w http.ResponseWriter, r *http.Request) {
	token, err := get_token(w, r)
	if err != nil {
		return
	}
	if r.Method == "GET" {
		get_orders(w, token)
	} else if r.Method == "POST" {
		js, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte(`{
				"code": "EMPTY_REQUEST",
				"message": "请求体为空"
			}`))
			return
		}
		var body BodyOrder;
		//if err := r.Body.Close(); err != nil { }
		if err := json.Unmarshal(js, &body); err != nil {
			w.WriteHeader(400)
			w.Write([]byte(`{
				"code": "MALFORMED_JSON",
				"message": "格式错误"
			}`))
			return
		}
		post_orders(w, token, body)
	} else {
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
}

// -------------------- Food --------------------
type Food struct {
	food_id int
	price int
	stock int
}
var foods_cache map[id]Food
// TODO: weaken the consistency
func foods(w http.ResponseWriter, r *http.Request) {
	// check GET
	token, err := get_token(w, r)
	if err != nil {
		return
	}
	ret := `[
	`
	for id, food := range foods_cache {
		ret += `{"id": ` + id + `, "price": ` + food.price + `, "stock": ` + food.stock + `},
		`
	}
	ret += `]`

	w.Write(ret.to_bytes())
}

// TODO: make things async
//  e.g. use goroutine to calc `total`
type User struct {
	user_id int
	username string
	// access_token string // hidden to map key
	done bool
	total int
	//order Cart // get async
}

type UserP struct {
	user_id int
	password string
}

var tokens [11001]string
var userps map[string]UserP

func gen_token() {
	for i := 0; i<11001; i++ {
		tokens[i] = RandStringBytes(10);
	}
}

// -------------------- MySql --------------------
var db *sql.DB
func cache_foods() {
	var id int
	var price int
	var stock int
	rows, err := db.Query("SELECT id, name, pass FROM food")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id, &price, &stock)
		if err != nil {
			log.Fatal(err)
		}
		foods_cache[id] = Food{stock, price}
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
}
func cache_users() {
	var id int
	var name string
	var pass string

	rows, err := db.Query("SELECT id, name, pass FROM user")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id, &name, &pass)
		if err != nil {
			log.Fatal(err)
		}
		userps[name] = UserP{id, pass}
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
}
func init_mysql() {
	db, err := sql.Open("mysql", "user:password@/dbname")
	if err != nil {
		panic(err.Error())
	}
	defer db.Close()
	err = db.Ping()
	if err != nil {
		panic(err.Error())
	}
}


// -------------------- main --------------------
// TODO: load foods at boot
// TODO: maybe: warm up db before starts
// TODO: make redis no-disk sync
func main() {
	host := os.Getenv("APP_HOST")
	port := os.Getenv("APP_PORT")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "8080"
	}
	addr := fmt.Sprintf("%s:%s", host, port)

	gen_token()
	init_mysql()
	cache_users()
	cache_foods()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world!"))
	})
	http.HandleFunc("/login", login)
	http.HandleFunc("/foods", foods)
	http.HandleFunc("/carts", carts)
	http.HandleFunc("/carts/:cart_id", carts)
	http.HandleFunc("/orders", orders)
	http.HandleFunc("/admin/orders", admin_orders)

	http.ListenAndServe(addr, nil)
}

func get_token(w http.ResponseWrite, r *http.Request) (string, error) {
	token, ok := r.URL.Query()["access_token"]
	if ok && token != "" {
		valid := redis_valid_token(token)
		if valid {
			return token, nil
		}
	}
	token = r.Header.Get("Access-Token")
	if token != "" {
		valid := redis_valid_token(token)
		if valid {
			return token, nil
		}
	}
	w.WriteHeader(401)
	w.Write([]byte(`{
		"code": "INVALID_ACCESS_TOKEN",
		"message": "无效的令牌"
	}`))
	return nil, errors.New()
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
