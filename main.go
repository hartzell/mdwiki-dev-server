package main

import (
	"net/http"

	"code.google.com/p/go.net/websocket"
	"encoding/json"
	"gopkg.in/fsnotify.v1"

	"flag"
	"log"
	"regexp"
	"time"
)

var (
	flagContentDir = flag.String("d", "./",
		"Directory from which to read files")
	flagNotifyRegexp = flag.String("r", ".*(md|html|css)$",
		"Regular expression that matches files to watch for changes")
)

func ok(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func webHandler(ws *websocket.Conn) {
	type ReloadMessage struct {
		R time.Time `json:"r"`
	}

	watcher, err := fsnotify.NewWatcher()
	ok(err)

	defer watcher.Close()

	err = watcher.Add(*flagContentDir)
	ok(err)

	for {
		select {
		case event := <-watcher.Events:
			matched, err :=
				regexp.MatchString(*flagNotifyRegexp, event.Name)
			ok(err)

			if !matched || event.Op&fsnotify.Chmod == fsnotify.Chmod {
				continue
			}

			message := ReloadMessage{R: time.Now()}
			b, err := json.Marshal(message)
			ok(err)

			log.Println("_reload sent because:", event)
			websocket.Message.Send(ws, string(b))

		case err := <-watcher.Errors:
			log.Println("error:", err)
		}
	}
}

func main() {
	flag.Parse()

	http.Handle("/_reloader", websocket.Handler(webHandler))
	http.Handle("/", http.FileServer(http.Dir(*flagContentDir)))

	log.Fatal(http.ListenAndServe(":8080", nil))
}
