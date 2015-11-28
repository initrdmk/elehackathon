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
	"sync/atomic"
	//	"github.com/julienschmidt/httprouter"
)

import "io/ioutil"
import "github.com/mediocregopher/radix.v2/pool"
import "database/sql"
import _ "github.com/go-sql-driver/mysql"

const debug = false

// -------------------- Redis --------------------
var redis *pool.Pool

func init_redis() {
	var err error
	host := os.Getenv("REDIS_HOST")
	port := os.Getenv("REDIS_PORT")
	redis, err = pool.New("tcp", host+":"+port, 1024)
	if err != nil {
		log.Println("init_redis")
		log.Fatal(err)
	}
	conn, err := redis.Get()
	if err != nil {
		log.Println("init_redis")
		log.Fatal(err)
	}
	defer redis.Put(conn)
	conn.Cmd("FLUSHDB")
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
	// TODO: check relogin
	//token := "t=" + body.Username
	token := "t=" + nextRand()

	conn, err := redis.Get()
	if err != nil {
		log.Println("login:")
		log.Fatal(err)
	}
	defer redis.Put(conn)
	conn.Cmd("MSETNX", "t:"+token, user.user_id, "u:"+user.user_id, token)

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

		if !strings.HasPrefix(cart_id, "c=") {
			//println(404)
			w.WriteHeader(404)
			w.Write([]byte(`{"code":"CART_NOT_FOUND","message":"篮子不存在"}`))
			return
		}

		patch_carts(w, token, cart_id, body)

	} else {
		println("WRONG ME")
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
}

func post_carts(w http.ResponseWriter, token string) {

	conn, err := redis.Get()
	if err != nil {
		log.Println("post_cart")
		log.Fatal(err)
	}
	defer redis.Put(conn)

	user_id, err := conn.Cmd("GET", "t:"+token).Str()
	if err != nil {
		if debug {
			log.Println(err)
		}
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	cart_id := "c=" + nextRand()
	conn.Cmd("SET", "c:"+cart_id, user_id)

	w.Write([]byte(fmt.Sprintf(`{"cart_id":"%s"}`, cart_id)))
}

func patch_carts(
	w http.ResponseWriter,
	token string,
	cart_id string,
	body BodyCart) {

	conn, err := redis.Get()
	if err != nil {
		log.Println("patch_cart")
		log.Fatal(err)
	}
	defer redis.Put(conn)

	l, err := conn.Cmd("MGET", "t:"+token, "c:"+cart_id).List()
	//fmt.Printf("[%s %s] %+v\n", token, cart_id, l)
	if err != nil {
		log.Println("patch_cart")
		log.Fatal(err)
	}
	if l[0] == "" {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	if l[1] == "" {
		w.WriteHeader(404)
		w.Write([]byte(`{"code":"CART_NOT_FOUND","message":"篮子不存在"}`))
		return
	}
	user_id := l[0]
	cart := strings.Split(l[1], " ")
	cart_user_id := cart[0]

	if user_id != cart_user_id {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"NOT_AUTHORIZED_TO_ACCESS_CART","message":"无权限访问指定的篮子" }`))
		return
	}

	tot_count := 0
	for _, food_str := range cart[1:] {
		ll := len(food_str)
		if ll <= 2 {
			tot_count++
		} else if food_str[ll-2] != '*' {
			tot_count++
		} else {
			tot_count += int(food_str[ll-1]) - '0'
		}
	}

	//fmt.Printf("%+v\n", cart)
	if tot_count+body.Count > 3 {
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
	if body.Count == 1 {
		if conn.Cmd("APPEND", "c:"+cart_id, " "+strconv.Itoa(body.Food_id)).Err != nil {
			log.Println("patch_cart")
			log.Fatal(err)
		}
	} else {
		if conn.Cmd("APPEND", "c:"+cart_id, " "+strconv.Itoa(body.Food_id)+"*"+string(body.Count+'0')).Err != nil {
			log.Println("patch_cart")
			log.Fatal(err)
		}
	}
	w.WriteHeader(204)
	w.Write([]byte(""))
}

// -------------------- Order --------------------
type BodyOrder struct {
	Cart_id string
}

func post_orders(w http.ResponseWriter, token string, body BodyOrder) {

	conn, err := redis.Get()
	if err != nil {
		log.Println("post_orders")
		log.Fatal(err)
	}
	defer redis.Put(conn)

	l, _ := conn.Cmd("MGET", "t:"+token, "c:"+body.Cart_id, "o:"+token).List()
	if l[0] == "" {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"INVALID_ACCESS_TOKEN","message":"无效的令牌"}`))
		return
	}
	if l[1] == "" {
		w.WriteHeader(404)
		w.Write([]byte(`{"code":"CART_NOT_FOUND","message":"篮子不存在"}`))
		return
	}
	if l[2] != "" {
		w.WriteHeader(403)
		w.Write([]byte(`{"code":"ORDER_OUT_OF_LIMIT","message":"每个用户只能下一单"}`))
		return
	}
	user_id := l[0]
	cart := strings.Split(l[1], " ")
	cart_user_id := cart[0]
	cart = cart[1:]

	if user_id != cart_user_id {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"NOT_AUTHORIZED_TO_ACCESS_CART","message":"无权限访问指定的篮子" }`))
		return
	}

	// ==================== commit ====================
	order := fmt.Sprintf(`{"id":"%s","user_id":%s,"items":[`, token, user_id)
	total := 0
	null := true
	//conn.PipeAppend("MULTI")
	//fmt.Printf("%+v\n", cart)
	counts := make([]int, len(cart))
	for i, food_str := range cart {
		ll := len(food_str)
		counts[i] = 1
		if ll <= 2 {
		} else if food_str[ll-2] != '*' {
		} else {
			counts[i] = int(food_str[ll-1]) - '0'
			cart[i] = food_str[:ll-2]
		}
		if !null {
			order += ","
		}
		order += fmt.Sprintf(`{"food_id":%s,"count":%d}`, cart[i], counts[i])
		ifood_id, _ := strconv.Atoi(cart[i])
		total += foods_cache[ifood_id].price * counts[i]
		null = false
	}
	order += fmt.Sprintf(`],"total":%d}`, total)

	for i, food_id := range cart {
		conn.PipeAppend("DECRBY", "f:"+food_id, counts[i])
	}
	conn.PipeAppend("SET", "o:"+token, order)
	succ := true
	for _ = range cart {
		c, _ := conn.PipeResp().Int()
		if c < 0 {
			succ = false
			//fmt.Printf("%+v %+v\n", cart, counts)
			//fmt.Println(c)
		}
	}
	conn.PipeResp()
	if !succ {
		// async roll back
		go func(cart []string, counts []int, token string) {
			conn, err := redis.Get()
			if err != nil {
				log.Println("post_orders")
				log.Fatal(err)
			}
			defer redis.Put(conn)

			for i, food_id := range cart {
				conn.PipeAppend("INCBY", "f:"+food_id, counts[i])
			}
			conn.PipeAppend("DEL", "o:"+token)
			for _ = range cart {
				conn.PipeResp()
			}
			conn.PipeResp()
		}(cart, counts, token)
		w.WriteHeader(403)
		//println("insucc")
		w.Write([]byte(`{"code": "FOOD_OUT_OF_STOCK","message": "食物库存不足"}`))
		return
	}
	w.Write([]byte(fmt.Sprintf(`{"id":"%s"}`, token)))
}

func get_orders(w http.ResponseWriter, token string) {
	conn, err := redis.Get()
	if err != nil {
		log.Println("get_orders")
		log.Fatal(err)
	}
	defer redis.Put(conn)
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
	conn, err := redis.Get()
	if err != nil {
		log.Println("admin_orders")
		log.Fatal(err)
	}
	defer redis.Put(conn)

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

// -------------------- Food --------------------
type Food struct {
	//	food_id int
	price int
	stock int
}

var done_orders map[string]string
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

const TOKEN_COUNT = 301001
const TOKEN_LEN = 24

var tokens [TOKEN_COUNT]string
var userps map[string]UserP
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

	conn, err := redis.Get()
	if err != nil {
		log.Println("cache_foods")
		log.Fatal(err)
	}
	defer redis.Put(conn)
	for k, v := range foods_cache {
		conn.Cmd("SET", "f:"+strconv.Itoa(k), v.stock)
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
		userps[name] = UserP{strconv.Itoa(id), pass}
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
