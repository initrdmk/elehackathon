#!/bin/sh
killall go
killall main
wait 3
redis-cli flushdb
wait 1
APP_PORT=8081 go run main.go &
APP_PORT=8082 go run main.go &
APP_PORT=8083 go run main.go &
