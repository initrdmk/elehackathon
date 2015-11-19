// Go hello-world implementation for eleme/hackathon.

package main

import (
	"fmt"
	"net/http"
	"os"
	"redis"
)



func redis_set(key string, value string) {

}

func redis_get(key string) {

}

// TODO: weaken the consistency
func foods(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.Write([]byte("Wrong Method"))
		return
	}


	ret := `[
	`

	for i in foods {
		ret += `{"id": ` + id + `, "price": ` + price + `, "stock": ` + stock + `99},
		`
	}
	ret += `]`

	w.Write(ret.to_bytes())
}

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Write([]byte("Wrong Method"))
		return
	}
	r.ParseForm()
	username := u.Form["username"]
	password := u.Form["password"]
	uid, err := login_db(username, password)
	if err != nil {
		w.Write([]byte(`{
			"code": "USER_AUTH_FAIL",
			"message": "用户名或密码错误"
		}`))
		return
	}
	access_token := "acctoken" + RandStringBytes(10)
	// TODO: multi redis instance, each for one table?
	redis_set(access_token, uid)
	w.Write([]byte(`{
		"user_id": ` + uid + `,
		"username": "` + username + `",
		"access_token": "` + access_token + `"
	}`))
}

func carts(w http.ResponseWriter, r *http.Request) {
	token, err := getToken(w, r)
	if err != nil {
		return
	}
	if r.Method == "POST" {
		post_orders(w, token)
	} else if r.Method == "PATCH" {
		patch_orders(w, token)
	} else {
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
}

func post_carts(w http.ResponseWriter, token string) {
	cart_id = "cart1d" + RandStringBytes(11)
	w.Write([]byte(`{
		"cart_id": ` + cart_id + `
	}`))
}

func patch_carts(w http.ResponseWriter, token string) {
}

func post_orders(w http.ResponseWriter, token string) {

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
	token, err := getToken(w, r)
	if err != nil {
		return
	}
	if r.Method == "GET" {
		get_orders(w, token)
	} else if r.Method == "POST" {
		post_orders(w, token)
	} else {
		// TODO: comment out all redundant checks
		w.Write([]byte("Wrong Method"))
	}
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

	// access_token User
	m := make(map[string]User)

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

func getToken(w http.ResponseWrite, r *http.Request) (string, error) {
	w.Write();
	return "sdf", nil
	return nil, new Error()
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
