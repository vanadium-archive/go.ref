// Package stored is a storage server using the todos/schema schema.
//
// We need a schema-specific store because the current store implementation does
// not support unregistered types.
// TODO(jyh): Support unregistered types and remove this server.
//
// Usage:
//   stored [--name=<mount>] [--db=<dbName>]
//     - <name> is the Veyron mount point name, default /global/vstore/<hostname>/<username>.
//     - <dbName> is the filename in which to store the data.
//
// NOTE: For now, to make this demo work, replace "return ErrUntrustedKey" with
// "return nil" in "IsTrusted" in
// veyron/runtimes/google/security/keys/trusted_keys.go.
//
// The Store service has Veyron name, <name>/.store. Individual values with
// path <path> have name <name>/<path>.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"

	_ "veyron/examples/todos/schema" // Register the todos/schema types.
	"veyron/services/store/server"

	"veyron2/rt"
	"veyron2/security"
)

var (
	mountName string
	dbName    = flag.String("db", "/var/tmp/todos.db", "Store database dir")

	// TODO(jyh): Figure out how to get a real public ID.
	rootPublicID security.PublicID = security.FakePrivateID("anonymous").PublicID()
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
	// TODO(sadovsky): Change this to be the correct veyron2 path.
	dir := "global/vstore/" + hostname + "/" + username
	flag.StringVar(&mountName, "name", dir, "Mount point for store")
}

// main starts the store service, taking arguments from the command line flags.
func main() {
	r := rt.Init()

	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("r.NewServer() failed: ", err)
	}

	// Create a new StoreService.
	storeService, err := server.New(server.ServerConfig{Admin: rootPublicID, DBName: *dbName})
	if err != nil {
		log.Fatal("server.New() failed: ", err)
	}
	defer storeService.Close()

	// Register the services.
	storeDisp := server.NewStoreDispatcher(storeService)
	objectDisp := server.NewObjectDispatcher(storeService)
	if err := s.Register(".store", storeDisp); err != nil {
		log.Fatal("s.Register(storeDisp) failed: ", err)
	}
	if err := s.Register("", objectDisp); err != nil {
		log.Fatal("s.Register(objectDisp) failed: ", err)
	}

	// Create an endpoint and start listening.
	ep, err := s.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal("s.Listen() failed: ", err)
	}

	// Publish the service in the mount table.
	fmt.Printf("Endpoint: %s\n", ep)
	fmt.Printf("Name: %s\n", mountName)
	fmt.Printf("Example commands:\n")
	fmt.Printf("./bin/todos_init --data-path=src/veyron/examples/todos/todos_init/data.json \"--store=/%s\"\n", ep)
	fmt.Printf("./bin/todos_appd \"--store=/%s\"\n", ep)
	if err := s.Publish(mountName); err != nil {
		log.Fatal("s.Publish() failed: ", err)
	}

	// Wait forever.
	done := make(chan struct{})
	<-done
}
