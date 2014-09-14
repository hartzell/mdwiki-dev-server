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
<!-- From: https://www.npmjs.org/package/node-live-reload -->
<!-- Inserted by mdwiki-dev-server                        -->
<script>
var ws;
function socket() {
  ws = new WebSocket("ws://127.0.0.1:8080/_reloader");
  ws.onmessage = function ( e ) {
    var data = JSON.parse(e.data);
    if ( data.r ) {
      ws.close();
      location.reload();
    }
  };
}
setInterval(function () {
  if ( ws ) {
    if ( ws.readyState !== 1 ) {
      ws.close();
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

// newTicker starts a ticker goroutine that creates two channels
// (ticker, tickerShutdown) then wakes up every once in a while and
// sends a message on to its "ticker" channel.  It listens for a
// message on its tickerShutdown channel and exits if/when it receives
// one.
func newTicker(d time.Duration) (chan bool, chan bool) {
	ticker := make(chan bool)
	tickerShutdown := make(chan bool)

	go func() {
	Loop:
		for {
			time.Sleep(d)
			select {
			case ticker <- true:
			case <-tickerShutdown:
				break Loop
			default:
			}
		}
	}()
	return ticker, tickerShutdown
}

// newWatcher starts a goroutine that sends notifications about
// changes within a directory.  It returns two channels: notifier, on
// which it sends the fsnotify event as a string; and
// notifierShutdown, on which it listens for a message telling it to
// shutdown.
//
// It takes two arguments, a directory name to watch (string) and a
// regular expression which names much match in order to cause a
// notification.
func newWatcher(dir string, matchPattern string) (chan string, chan bool) {
	notifier := make(chan string)
	notifierShutdown := make(chan bool)

	go func() {
		watcher, err := fsnotify.NewWatcher()
		maybeBail(err)
		defer watcher.Close()

		err = watcher.Add(dir)
		maybeBail(err)

	Loop:
		for {
			select {
			case event := <-watcher.Events:
				matched, err := regexp.MatchString(matchPattern, event.Name)
				maybeBail(err)

				if !matched || event.Op&fsnotify.Chmod == fsnotify.Chmod {
					continue
				}
				notifier <- event.String()
			case <-notifierShutdown:
				break Loop
			case err := <-watcher.Errors:
				log.Println("error in filesystem watcher:", err)
			}
		}
	}()
	return notifier, notifierShutdown
}

// newReloadMessage returns an instance of the message packet that the
// node-live-reload javascript expects, as a JSON string.
func newReloadMessage() (message string) {
	type reloadMessage struct {
		R time.Time `json:"r"`
	}

	b, err := json.Marshal(reloadMessage{R: time.Now()})
	maybeBail(err)
	message = string(b)
	return message
}

func webHandler(ws *websocket.Conn) {
	log.Println("Entering webHandler")

	ticker, tickerShutdown := newTicker(1 * time.Second)
	notifier, notifierShutdown := newWatcher(*flagContentDir, *flagNotifyRegexp)

	var somethingChanged bool = false
Loop:
	for {
		select {
		case note := <-notifier:
			log.Println("reload needed because:", note)
			somethingChanged = true
		case _ = <-ticker:
			if somethingChanged == true {
				m := newReloadMessage()
				log.Println("sending reload message: " + m)

				err := websocket.Message.Send(ws, m)
				maybeBail(err)

				somethingChanged = false
				tickerShutdown <- true
				notifierShutdown <- true
				break Loop
			}
		}
	}
	log.Println("Leaving webHandler")
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
