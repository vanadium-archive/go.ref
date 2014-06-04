// TODO(sadovsky): Use utils from veyron/rt/blackbox/util_test.go.

package test

import (
	"crypto/rand"
	"io/ioutil"
	"log"
	"os"

	"veyron/services/store/server"

	"veyron2"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/storage/vstore"
)

// getRuntime initializes the veyron2.Runtime if needed, then returns it.
func getRuntime() veyron2.Runtime {
	// returns Runtime if already initialized
	return rt.Init(veyron2.LocalID(security.FakePrivateID("todos")))
}

// startServer starts a store server and returns the server name as well as a
// function to close the server.
func startServer() (string, func()) {
	r := getRuntime()

	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("r.NewServer() failed: ", err)
	}

	// Make dbName.
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		log.Fatal("rand.Read() failed: ", err)
	}
	dbDir, err := ioutil.TempDir("", "db")
	if err != nil {
		log.Fatal("ioutil.TempDir() failed: ", err)
	}

	// Create a new StoreService.
	// TODO(ashankar): This "anonymous" is a special string that VCs
	// default to.  This really should be the identity used by the runtime.
	storeService, err := server.New(server.ServerConfig{Admin: r.Identity().PublicID(), DBName: dbDir})
	if err != nil {
		log.Fatal("server.New() failed: ", err)
	}

	// Register the services.
	storeDisp := server.NewStoreDispatcher(storeService, nil)
	if err := s.Register("", storeDisp); err != nil {
		log.Fatal("s.Register(storeDisp) failed: ", err)
	}

	// Create an endpoint and start listening.
	ep, err := s.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal("s.Listen() failed: ", err)
	}

	return naming.JoinAddressName(ep.String(), ""), func() {
		s.Stop()
		os.Remove(dbDir)
	}
}

// makeClient creates a client and returns a function to close the client.
func makeClient(name string) (storage.Store, func()) {
	getRuntime() // verify that Runtime was initialized
	st, err := vstore.New(name)
	if err != nil {
		log.Fatal("vstore.New() failed: ", err)
	}
	cl := func() {
		if err := st.Close(); err != nil {
			log.Fatal("st.Close() failed: ", err)
		}
	}
	return st, cl
}

// startServerAndMakeClient calls startServer and makeClient. It returns the
// client as well as a function to close everything.
func startServerAndMakeClient() (storage.Store, func()) {
	mount, clServer := startServer()
	st, clClient := makeClient(mount)
	cl := func() {
		clServer()
		clClient()
	}
	return st, cl
}
