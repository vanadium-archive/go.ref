// todos_appd is a web application backed by a Veyron store.
//
// For now, it simply displays the raw contents of the store.

// TODO(sadovsky): Implement actual app, using Veyron {store,query,watch,sync}
// over veyron.js.
package main

import (
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"path"

	"veyron/examples/storage/viewer"
	_ "veyron/examples/todos/schema" // Register the todos/schema types.
	"veyron2/rt"
	"veyron2/storage/vstore"
)

var (
	storeName string
	port      = flag.Int("port", 10000, "IPV4 port number to serve")
	useViewer = flag.Bool("useViewer", false, "If true, serve viewer instead")
)

var rootDir = path.Join(
	"/usr/local/google/home/sadovsky",
	"veyron/v0/v/src/veyron/examples/todos/todos_appd")

func init() {
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	hostname := "unknown"
	if h, err := os.Hostname(); err == nil {
		hostname = h
	}
	// TODO(sadovsky): Change this to be the correct veyron2 path.
	dir := "global/vstore/" + hostname + "/" + username
	flag.StringVar(&storeName, "store", dir, "Name of the Veyron store")
}

func serveViewer() {
	log.Printf("Binding to store on %s", storeName)
	st, err := vstore.New(storeName)
	if err != nil {
		log.Fatalf("Can't connect to store: %s: %s", storeName, err)
	}

	viewer.ListenAndServe(fmt.Sprintf(":%d", *port), st)
}

func renderTemplate(w io.Writer, basename string, data interface{}) {
	filename := path.Join(rootDir, "templates", basename)
	t, err := template.ParseFiles(filename)
	if err != nil {
		panic(fmt.Sprintf("ParseFiles failed: %s", filename))
	}
	t.Execute(w, data)
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "index.html", nil)
}

func wrap(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if data := recover(); data != nil {
				http.Error(w, fmt.Sprint(data), http.StatusInternalServerError)
			}
		}()
		fn(w, r)
	}
}

func main() {
	rt.Init()

	if *useViewer {
		serveViewer()
	} else {
		http.HandleFunc("/", wrap(handleHome))
		http.Handle("/css/", http.FileServer(http.Dir(rootDir)))
		http.Handle("/js/", http.FileServer(http.Dir(rootDir)))
		fmt.Printf("Server running at http://localhost:%d\n", *port)
		http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	}
}
