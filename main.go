package main

import (
	"flag"
	"log"
	"net/http"
)

var flagDir =
	flag.String("d", "./", "Directory from which to read files")

func main() {
	flag.Parse();
	http.Handle("/", http.FileServer(http.Dir(*flagDir)))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
