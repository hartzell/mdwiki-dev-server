package main

import (
	"log"
	"net/http"
)

func main() {
	http.Handle("/", http.FileServer(http.Dir("/Users/hartzell/tmp/foo")))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
