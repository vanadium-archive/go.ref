// todos_appd is a web application backed by a Veyron store.
//
// It doesn't work yet, but it will soon. :)

// TODO(sadovsky): Implement actual app, using Veyron {store,query,watch,sync}
// over veyron.js.
package main

import (
	"flag"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/user"
	"path"

	_ "veyron/examples/todos/schema" // Register the todos/schema types.
	"veyron2/rt"
)

var (
	storeName string
	port      = flag.Int("port", 10000, "IPV4 port to serve")
)

var rootDir = path.Join(
	"/usr/local/google/home/sadovsky",
	"veyron/v0/go/src/veyron/examples/todos/todos_appd")

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

	http.HandleFunc("/", wrap(handleHome))
	http.Handle("/css/", http.FileServer(http.Dir(rootDir)))
	http.Handle("/js/", http.FileServer(http.Dir(rootDir)))

	fmt.Printf("Server running at http://localhost:%d\n", *port)
	http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
}
