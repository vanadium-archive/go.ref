// mdbd provides a UI for the mdb/schema movie database.
//
// The main purpose of this server is to register the types for the mdb/schema
// so that the html/templates work correctly.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"

	// Register the mdb/schema types.
	_ "veyron/examples/storage/mdb/schema"
	"veyron/examples/storage/viewer"
	"veyron2/rt"
	"veyron2/storage/vstore"
)

var (
	storeName string
	port      = flag.Int("port", 10000, "IPV4 port number to serve")
)

func init() {
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	hostname := "unknown"
	if h, err := os.Hostname(); err == nil {
		hostname = h
	}
	dir := "global/vstore/" + hostname + "/" + username
	flag.StringVar(&storeName, "store", dir, "Name of the Veyron store")
}

func main() {
	rt.Init()

	log.Printf("Binding to store on %s", storeName)
	st, err := vstore.New(storeName)
	if err != nil {
		log.Fatalf("Can't connect to store: %s: %s", storeName, err)
	}

	viewer.ListenAndServe(fmt.Sprintf(":%d", *port), st)
}
