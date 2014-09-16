package main

import (
	"net/http"
	"net/http/httptest"

	"code.google.com/p/go.net/websocket"
	"encoding/json"
	"gopkg.in/fsnotify.v1"

	"bytes"
	"flag"
	"github.com/op/go-logging"
	"os"
	"regexp"
	"strconv"
	"time"
)

var (
	flagContentDir = flag.String("dir", "./",
		"Directory from which to read files")
	flagNotifyRegexp = flag.String("regexp", ".*(md|html|css)$",
		"Regular expression that matches files to watch for changes")
	flagVerbose = flag.Bool("verbose", false, "foo")
	flagDebug   = flag.Bool("debug", false, "foo")

	log = logging.MustGetLogger("mdwiki-dev-server")
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

func setupLogging(level logging.Level) {
	var format = "%{color}%{time:15:04:05.000000} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}"

	// Setup one stderr and one syslog backend and combine them both into one
	// logging backend. By default stderr is used with the standard log flag.
	logBackend := logging.NewLogBackend(os.Stderr, "", 0)
	logging.SetBackend(logBackend)
	logging.SetFormatter(logging.MustStringFormatter(format))

	logging.SetLevel(level, "mdwiki-dev-server")
}

func maybeBail(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// keep track of tickers, useful for debugging
var tickerId = 1

// newTicker starts a ticker goroutine that creates two channels
// (ticker, tickerShutdown) then wakes up every once in a while and
// sends a message on to its "ticker" channel.  It listens for a
// message on its tickerShutdown channel and exits if/when it receives
// one.
func newTicker(d time.Duration) (chan bool, chan interface{}) {
	ticker := make(chan bool)
	tickerShutdown := make(chan interface{})

	go func() {
		myId := tickerId
		tickerId++
	Loop:
		for {
			time.Sleep(d)
			select {
			case ticker <- true:
				log.Debug("ticker (%d) fired", myId)
			case <-tickerShutdown:
				log.Debug("ticker (%d) got shutdown message", myId)
				break Loop
			default:
			}
		}
	}()
	return ticker, tickerShutdown
}

// keep track of watchers, useful for debugging.
var watcherId int = 1

// newWatcher starts a goroutine that sends notifications about
// changes within a directory.  It returns two channels: notifier, on
// which it sends the fsnotify event as a string; and
// notifierShutdown, on which it listens for a message telling it to
// shutdown.
//
// It takes two arguments, a directory name to watch (string) and a
// regular expression which names much match in order to cause a
// notification.
func newWatcher(dir string, matchPattern string) (chan string, chan interface{}) {
	notifier := make(chan string)
	notifierShutdown := make(chan interface{})

	go func() {
		myId := watcherId
		watcherId++

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
				log.Debug("notifier(%d) saw %s", myId, event.String())
			case <-notifierShutdown:
				break Loop
			case err := <-watcher.Errors:
				log.Error("error in filesystem watcher: %s", err)
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
	log.Debug("Entering webHandler")

	ticker, tickerShutdown := newTicker(1 * time.Second)
	notifier, notifierShutdown := newWatcher(*flagContentDir, *flagNotifyRegexp)

	var somethingChanged bool = false
Loop:
	for {
		select {
		case note := <-notifier:
			log.Notice("reload needed because: %s", note)
			somethingChanged = true
		case _ = <-ticker:
			log.Debug("handling ticker")
			if somethingChanged == true {
				m := newReloadMessage()
				log.Notice("sending reload message: %s", m)

				err := websocket.Message.Send(ws, m)
				maybeBail(err)

				somethingChanged = false
				close(tickerShutdown)
			        close(notifierShutdown)
				break Loop
			}
		}
	}
	log.Debug("Leaving webHandler")
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

	log.Debug("serving: %s", r.URL.String())
	recorder := httptest.NewRecorder()
	h := http.FileServer(f.root)
	h.ServeHTTP(recorder, r)

	// we copy the original headers first
	for k, v := range recorder.Header() {
		//
		if k ==  "Last-Modified" || k == "ETag" {
			log.Debug("skipping cache control header: %s", k)
			continue
		}
		log.Debug("%s: %s", k, v)
		w.Header()[k] = v
	}

	// is content HTML?
	contentType := w.Header().Get("Content-Type")
	log.Debug("content type is %s", contentType)
	isHTML, err := regexp.MatchString("^text/html.*", contentType)
	maybeBail(err)

	// does content contain our marker (and where is it?)?
	i := bytes.Index(recorder.Body.Bytes(), []byte("</head>"))
	log.Debug("splice location found at position %d", i)

	if isHTML && i >= 0 {
		// Kilroy was here
		log.Notice("serving modified content for " + r.URL.Path)
		w.Header().Set("X-Via-FilteringFileServer", "Filtered")

		// update Content-Length header with correct value
		w.Header().Set("Content-Length",
			strconv.Itoa(len(recorder.Body.Bytes())+len(snippet)))

		// write body with snippet spliced in
		_, err = w.Write(recorder.Body.Bytes()[:i])
		maybeBail(err)
		_, err = w.Write([]byte(snippet))
		maybeBail(err)
		_, err = w.Write(recorder.Body.Bytes()[i:])
		maybeBail(err)
	} else {
		// Kilroy was here
		log.Notice("serving unaltered content for " + r.URL.Path)
		w.Header().Set("X-Via-FilteringFileServer", "Skipped")

		// send the original body
		_, err = w.Write(recorder.Body.Bytes())
		maybeBail(err)
	}
}

func main() {
	flag.Parse()

	if *flagVerbose {
		setupLogging(logging.INFO)
	} else if *flagDebug {
		setupLogging(logging.DEBUG)
	} else {
		setupLogging(logging.ERROR)
	}

	http.Handle("/_reloader", websocket.Handler(webHandler))
	http.Handle("/", FilteringFileServer(http.Dir(*flagContentDir)))

	log.Fatal(http.ListenAndServe(":8080", nil))
}
