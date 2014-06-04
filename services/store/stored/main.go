// stored is a storage server.
//
// Usage:
//
//     stored [--name=<mount>] [--db=<dbName>]
//
//     - <name> is the Veyron mount point name, default /global/vstore/<hostname>/<username>.
//     - <dbName> is the filename in which to store the data.
//
// The store service has Veyron name, <name>/.store.
// The raw store service has Veyron name, <name>/.store.raw.
// Individual values with path <path> have name <name>/<path>.
package main

import (
	"flag"
	"log"
	"os"
	"os/user"

	vflag "veyron/security/flag"
	"veyron/services/store/server"

	"veyron2/rt"

	_ "veyron/services/store/typeregistryhack"
)

var (
	mountName string
	dbName    = flag.String("db", "/var/tmp/veyron_store.db", "Metadata database")
	// TODO(rthellend): Remove the address flag when the config manager is working.
	address = flag.String("address", ":0", "Address to listen on.")
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
	flag.StringVar(&mountName, "name", dir, "Mount point for media")
}

// Main starts the content service, taking arguments from the command line
// flags.
func main() {
	r := rt.Init()

	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("r.NewServer() failed: ", err)
	}

	// Create a new StoreService.
	storeService, err := server.New(server.ServerConfig{Admin: r.Identity().PublicID(), DBName: *dbName})
	if err != nil {
		log.Fatal("server.New() failed: ", err)
	}
	defer storeService.Close()

	// Create the authorizer.
	auth := vflag.NewAuthorizerOrDie()

	// Register the services.
	storeDisp := server.NewStoreDispatcher(storeService, auth)
	if err := s.Register("", storeDisp); err != nil {
		log.Fatal("s.Register(storeDisp) failed: ", err)
	}

	// Create an endpoint and start listening.
	ep, err := s.Listen("tcp", *address)
	if err != nil {
		log.Fatal("s.Listen() failed: ", err)
	}

	// Publish the service in the mount table.
	log.Printf("Mounting store on %s, endpoint /%s", mountName, ep)
	if err := s.Publish(mountName); err != nil {
		log.Fatal("s.Publish() failed: ", err)
	}

	// Wait forever.
	done := make(chan struct{})
	<-done
}
