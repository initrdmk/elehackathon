package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
)

import "io/ioutil"
import "github.com/mediocregopher/radix.v2/pool"
import "database/sql"
import _ "github.com/go-sql-driver/mysql"

// -------------------- Redis --------------------
var redis *pool.Pool

func init_redis() {
	var err error
	host := os.Getenv("REDIS_HOST")
	port := os.Getenv("REDIS_PORT")
	redis, err = pool.New("tcp", host+":"+port, 10)
	if err != nil {
		log.Fatal(err)
	}
}

func redis_add_token(token string, uid int) {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)
	if conn.Cmd("SET", "t2u:"+token, uid).Err != nil {
		log.Fatal(err)
	}
}

// TODO: could be pre allocated
func redis_add_cart(token string, cart_id string) {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	if conn.Cmd("SET", "t2c:"+token, cart_id).Err != nil {
		log.Fatal(err)
	}
	redis.Put(conn)
}

type Cart struct {
	cart_id string
	token   string
	count   int
}

func redis_get_cart(token string, cart_id string) ([]string, error) {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)
	r := conn.Cmd("SMEMBER", "c:"+cart_id)
	if r.Err != nil {
		log.Fatal(err)
	}
	return r.List()
}

func redis_add_to_cart(cart_id string, food_id int, count int) {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)
	if conn.Cmd("SADD", "c:"+cart_id, food_id).Err != nil {
		log.Fatal(err)
	}
}

func redis_commit_order(token string, cart_id string) error {
	//err := redis_commmit_order(cart)
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)
	r := conn.Cmd("GET", "t2u:"+token)
	r := conn.Cmd("GET", "t2c:"+token)

	order := fmt.Sprintf(`[{"id": "%s", "user_id":%d, "items":[`, "123", uid)
	r := conn.Cmd("SMEMBER", "c:"+cart_id)
	if r.Err != nil {
		log.Fatal(r.Err)
	}
	total := 0
	for food_id := range r.List() {
		r = conn.Cmd("SUB", "f:"+food_id)
		if r.Err != nil {
			log.Fatal(r.Err)
		}
		order += fmt.Sprintf(`{"food_id":%d,"count":%d},`, food_id, count)
		total += foods_cache[food_id].price * count
	}
	order += fmt.Sprintf(`],"total":%d}]`, total)
	r = conn.Cmd("SET", "o:"+uid, order)
	if r.Err != nil {
		log.Fatal(r.Err)
	}
	return nil
}

func redis_get_order(order string) (string, bool) {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)
	if conn.Cmd("GET", "o:"+cart_id, food_id).Err != nil {
		log.Fatal(err)
	}
	return "1", true
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
	var body BodyLogin
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
	count   int
}
type BodyCartId struct {
	cart_id string
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
		var body BodyCart
		//if err := r.Body.Close(); err != nil { }
		if err := json.Unmarshal(js, &body); err != nil {
			w.WriteHeader(400)
			w.Write([]byte(`{
				"code": "MALFORMED_JSON",
				"message": "格式错误"
			}`))
			return
		}
		cart_id := strings.TrimPrefix(r.RequestURI, "/carts/")
		patch_carts(w, token, cart_id, body)

	} else {
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
}

func post_carts(w http.ResponseWriter, token string) {
	cart_id := "cart1d" + RandStringBytes(11)
	redis_add_cart(token, cart_id)
	w.Write([]byte(fmt.Sprintf(`{
		"cart_id": %d
	}`, cart_id)))
}

func patch_carts(
	w http.ResponseWriter,
	token string,
	cart_id string,
	body BodyCart) {

	cart := redis_get_cart(token, cart_id)
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

	_, ok := foods_cache[body.food_id]
	if !ok {
		w.WriteHeader(404)
		w.Write([]byte(`{
			"code": "FOOD_NOT_FOUND",
			"message": "食物不存在"
		}`))
		return
	}
	redis_add_to_cart(cart_id, body.food_id, body.count)
	w.WriteHeader(204)
}

// -------------------- Order --------------------
type BodyOrder struct {
	cart_id string
}

func post_orders(w http.ResponseWriter, token string, body BodyOrder) {
	cart := redis_get_cart(token, body.cart_id)
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
	//if token.done {
	if false {
		w.WriteHeader(403)
		w.Write([]byte(`{
			"code": "ORDER_OUT_OF_LIMIT",
			"message": "每个用户只能下一单"
		}`))
	}
	err := redis_commit_order(token, body.cart_id)
	if err != nil {
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
	order, ok := token2order[token]
	if ok {
		w.Write([]byte(order))
		return
	}
	order, ok = redis_get_order("done_order" + token)
	if ok {
		token2order[token] = order
		w.Write([]byte(order))
		return
	}
	w.Write([]byte(""))
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
		var body BodyOrder
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
	//	food_id int
	price int
	stock int
}

var foods_cache map[int]Food

// TODO: weaken the consistency
func foods(w http.ResponseWriter, r *http.Request) {
	// check GET
	_, err := get_token(w, r)
	if err != nil {
		return
	}
	ret := `[
	`
	for id, food := range foods_cache {
		ret += fmt.Sprintf(`{"id": %d, "price": %d, "stock": %d},
		`, id, food.price, food.stock)
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
	user_id  int
	password string
}

var tokens [11001]string
var userps map[string]UserP
var token2order map[string]string

func gen_token() {
	for i := 0; i < 11001; i++ {
		tokens[i] = RandStringBytes(10)
	}
}

// -------------------- MySql --------------------
var db *sql.DB

func cache_foods() {
	var id int
	var price int
	var stock int
	foods_cache = map[int]Food{}
	rows, err := db.Query("SELECT id, stock, price FROM food")
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
	log.Println("food cached")
}
func cache_users() {
	var id int
	var name string
	var pass string
	userps = map[string]UserP{}

	rows, err := db.Query("SELECT id, name, password FROM user")
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
	log.Println("user cached")
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
	//http.HandleFunc("/admin/orders", admin_orders)

	log.Println("serving...")
	http.ListenAndServe(addr, nil)
}

func redis_valid_token(token string) bool {
	return true
}

func get_token(w http.ResponseWriter, r *http.Request) (string, error) {
	token := r.URL.Query().Get("access_token")
	if token != string("") {
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
