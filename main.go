package main

import (
	"net/http"
	"net/http/httptest"

	"code.google.com/p/go.net/websocket"
	"encoding/json"
	"gopkg.in/fsnotify.v1"

	"bytes"
	"flag"
	"log"
	"regexp"
	"strconv"
	"time"
)

var (
	flagContentDir = flag.String("d", "./",
		"Directory from which to read files")
	flagNotifyRegexp = flag.String("r", ".*(md|html|css)$",
		"Regular expression that matches files to watch for changes")
)

var snippet string = `
<!-- Make sure to remove this in production -->
<!-- include it above the </body> tag -->
<script>
var ws;
function socket() {
  ws = new WebSocket("ws://127.0.0.1:8080/_reloader");
  ws.onmessage = function ( e ) {
    var data = JSON.parse(e.data);
    if ( data.r ) {
      location.reload();
    }
  };
}
setInterval(function () {
  if ( ws ) {
    if ( ws.readyState !== 1 ) {
      socket();
    }
  } else {
    socket();
  }
}, 1000);
</script>

`

func maybeBail(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func webHandler(ws *websocket.Conn) {
	type ReloadMessage struct {
		R time.Time `json:"r"`
	}

	watcher, err := fsnotify.NewWatcher()
	maybeBail(err)

	defer watcher.Close()

	err = watcher.Add(*flagContentDir)
	maybeBail(err)

	for {
		select {
		case event := <-watcher.Events:
			matched, err :=
				regexp.MatchString(*flagNotifyRegexp, event.Name)
			maybeBail(err)

			if !matched || event.Op&fsnotify.Chmod == fsnotify.Chmod {
				continue
			}

			message := ReloadMessage{R: time.Now()}
			b, err := json.Marshal(message)
			maybeBail(err)

			log.Println("_reload sent because:", event)
			websocket.Message.Send(ws, string(b))

		case err := <-watcher.Errors:
			log.Println("error:", err)
		}
	}
}

// A wrapper for the FileServer.  See
// http://justinas.org/writing-http-middleware-in-go/ and
// https://gist.github.com/justinas/7059324 but beware that he doesn't
// update the Content-Length header and doesn't check w.Write()'s
// return value, leading to confusion and sadness (and content not
// getting sent to the client...).

type filteringFileServer struct {
	root http.FileSystem
}

func FilteringFileServer(root http.FileSystem) http.Handler {
	return &filteringFileServer{root}
}

func (f *filteringFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error

	recorder := httptest.NewRecorder()
	h := http.FileServer(f.root)
	h.ServeHTTP(recorder, r)

	// we copy the original headers first
	for k, v := range recorder.Header() {
		w.Header()[k] = v
	}

	// is content HTML?
	contentType := w.Header().Get("Content-Type")
	isHTML, err := regexp.MatchString("^text/html.*", contentType)
	maybeBail(err)

	// does content contain our marker (and where is it?)?
	i := bytes.Index(recorder.Body.Bytes(), []byte("</head>"))

	if isHTML && i >= 0 {
		// Kilroy was here
		log.Println("Serving modified content for " + r.URL.Path)
		w.Header().Set("X-Via-FilteringFileServer", "Filtered")

		// update Content-Length header with correct value
		w.Header().Set("Content-Length",
			strconv.Itoa(len(recorder.Body.Bytes()) + len(snippet)))

		// write body with snippet spliced in
		_, err = w.Write(recorder.Body.Bytes()[:i])
		maybeBail(err)
		_, err = w.Write([]byte(snippet))
		maybeBail(err)
		_, err = w.Write(recorder.Body.Bytes()[i:])
		maybeBail(err)
	} else {
		// Kilroy was here
		log.Println("Serving unaltered content for " + r.URL.Path)
		w.Header().Set("X-Via-FilteringFileServer", "Skipped")

		// send the original body
		_, err = w.Write(recorder.Body.Bytes())
		maybeBail(err)
	}
}

func main() {
	flag.Parse()

	http.Handle("/_reloader", websocket.Handler(webHandler))
	http.Handle("/", FilteringFileServer(http.Dir(*flagContentDir)))

	log.Fatal(http.ListenAndServe(":8080", nil))
}
