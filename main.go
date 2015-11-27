package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
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

type TokenUserCart struct {
	token   string
	user_id string
	cart_id string
}

func redis_add_tuc(token string, uid string, cart_id string) error {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)
	r := conn.Cmd("MSETNX", "t:"+token, uid+"|"+cart_id,
		"u:"+uid, token+"|"+cart_id,
		"c:"+cart_id, "0")
	return r.Err
}

type Cart struct {
	cart_id string
	token   string
	count   int
}

// OPT: use token to index carts
// apply this immediately
func redis_get_cart(token string) ([]string, error) {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)
	s, err := conn.Cmd("GET", "c:"+token).Str()
	if err != nil {
		return nil, err
	}
	ss := strings.Split(s, " ")
	return ss[1:], nil
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
	Username string
	Password string
}

func login(w http.ResponseWriter, r *http.Request) {
	// check POST
	//body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1048576))
	js, _ := ioutil.ReadAll(r.Body)
	if len(js) == 0 {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"EMPTY_REQUEST","message":"请求体为空"}`))
		return
	}
	var body BodyLogin
	//if err := r.Body.Close(); err != nil { }
	if err := json.Unmarshal(js, &body); err != nil {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"MALFORMED_JSON","message":"格式错误"}`))
		return
	}

	user, ok := userps[body.Username]
	//log.Printf("uname: %s; upass: %s; uupass: %s\n", body.Username, user.password, body.Password)
	if !ok || user.password != body.Password {
		w.WriteHeader(403)
		w.Write([]byte(`{"code":"USER_AUTH_FAIL","message":"用户名或密码错误"}`))
		return
	}
	// TODO: check relogin
	token := "t=" + body.Username
	cart_id := "c=" + body.Username

	redis_add_tuc(token, user.user_id, cart_id)

	//log.Println("==== ")
	w.WriteHeader(200)
	//log.Printf(`{"user_id":%s,"username":"%s","access_token":"%s"}`,
	//	user.user_id, body.Username, token)
	w.Write([]byte(fmt.Sprintf(`{"user_id":%s,"username":"%s","access_token":"%s"}`,
		user.user_id, body.Username, token)))
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
		cart_id := strings.TrimPrefix(r.RequestURI, "/carts/")
		patch_carts(w, token, cart_id, body)

	} else {
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
}

func post_carts(w http.ResponseWriter, token string) {

	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)

	s, err := conn.Cmd("GET", "t:"+token).Str()
	if err != nil {
		log.Println(err)
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}

	w.Write([]byte(fmt.Sprintf(`{"cart_id":"%s"}`, strings.Split(s, "|")[1])))
}

func patch_carts(
	w http.ResponseWriter,
	token string,
	cart_id string,
	body BodyCart) {

	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)

	l, _ := conn.Cmd("MGET", "t:"+token, "c:"+token).List()
	if l[0] == "" {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	tuc := &TokenUserCart{}
	tuc.token = token
	ss := strings.Split(l[0], "|")
	tuc.user_id = ss[0]
	tuc.cart_id = ss[1]
	cart := strings.Split(l[1], " ")

	if false {
		// never check this
		w.WriteHeader(404)
		w.Write([]byte(`{"code":"CART_NOT_FOUND","message":"篮子不存在"}`))
		return
	}
	if tuc.cart_id != cart_id {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"NOT_AUTHORIZED_TO_ACCESS_CART","message":"无权限访问指定的篮子" }`))
		return
	}
	if len(cart) >= 3 {
		w.WriteHeader(403)
		w.Write([]byte(`{"code":"FOOD_OUT_OF_LIMIT","message":"篮子中食物数量超过了三个"}`))
		return
	}

	_, ok := foods_cache[body.Food_id]
	if !ok {
		w.WriteHeader(404)
		w.Write([]byte(`{"code":"FOOD_NOT_FOUND","message":"食物不存在"}`))
		return
	}
	if conn.Cmd("APPEND", "c:"+token, ""+strconv.Itoa(body.Food_id)).Err != nil {
		log.Fatal(err)
	}
	w.WriteHeader(204)
}

// -------------------- Order --------------------
type BodyOrder struct {
	Cart_id string
}

func post_orders(w http.ResponseWriter, token string, body BodyOrder) {

	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)

	l, _ := conn.Cmd("MGET", "t:"+token, "c:"+token).List()
	if l[0] == "" {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	tuc := &TokenUserCart{}
	tuc.token = token
	ss := strings.Split(l[0], "|")
	tuc.user_id = ss[0]
	tuc.cart_id = ss[1]
	cart := strings.Split(l[1], " ")

	if false {
		// never check this
		w.WriteHeader(404)
		w.Write([]byte(`{"code":"CART_NOT_FOUND","message":"篮子不存在"}`))
		return
	}
	if tuc.cart_id != body.Cart_id {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"NOT_AUTHORIZED_TO_ACCESS_CART","message":"无权限访问指定的篮子" }`))
		return
	}
	if len(cart) >= 3 {
		w.WriteHeader(403)
		w.Write([]byte(`{"code":"FOOD_OUT_OF_LIMIT","message":"篮子中食物数量超过了三个"}`))
		return
	}

	// ==================== commit ====================
	order := fmt.Sprintf(`[{"id":"%s","user_id":%s,"items":[`, "123", tuc.user_id)
	total := 0
	//conn.PipeAppend("MULTI")
	for food_id := range ss {
		conn.PipeAppend("DECR", "f:"+strconv.Itoa(food_id))
		// FIXME: check count
		count := 1
		order += fmt.Sprintf(`{"food_id":%d,"count":%d},`, food_id, count)
		total += foods_cache[food_id].price * count
	}
	order += fmt.Sprintf(`],"total":%d}]`, total)
	conn.PipeAppend("SET", "o:"+token, order)
	for _ = range ss {
		// FIXME: check insufficient
		conn.PipeResp()
	}
	conn.PipeResp()
	w.Write([]byte(`{"id ":"0"}`))
	return
	if false {
		// TODO: check duplicate
		w.WriteHeader(403)
		w.Write([]byte(`{
			"code": "ORDER_OUT_OF_LIMIT",
			"message": "每个用户只能下一单"
		}`))
	}
	if err != nil {
		// TODO
		w.WriteHeader(403)
		w.Write([]byte(`{"code": "FOOD_OUT_OF_STOCK","message": "食物库存不足"}`))
		return
	}
}

func get_orders(w http.ResponseWriter, token string) {
	conn, err := redis.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer redis.Put(conn)
	r, err := conn.Cmd("GET", "o:"+token).Str()
	if err != nil {
		log.Fatal(err)
	}
	w.Write([]byte(r))
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

// -------------------- Food --------------------
type Food struct {
	//	food_id int
	price int
	stock int
}

var foods_cache map[int]Food
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

	sorted_foods_keys = make([]int, 0)
	for k, _ := range foods_cache {
		sorted_foods_keys = append(sorted_foods_keys, k)
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
		userps[name] = UserP{strconv.Itoa(id), pass}
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
	init_redis()
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
	if token != ("") {
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
