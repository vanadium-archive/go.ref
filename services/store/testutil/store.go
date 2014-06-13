package testutil

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	istore "veyron/services/store/server"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
)

// NewStore creates a new testing instance of the store server and returns
// a veyron name that identifies the instance and a closure that can
// be used to terminate the instance and clean up.
func NewStore(t *testing.T, server ipc.Server, id security.PublicID) (string, func()) {
	// Create a temporary directory for the store server.
	prefix := "vstore-test-db"
	dbName, err := ioutil.TempDir("", prefix)
	if err != nil {
		t.Fatalf("TempDir(%v, %v) failed: %v", "", prefix, err)
	}

	// Create a new StoreService.
	config := istore.ServerConfig{Admin: id, DBName: dbName}
	storeService, err := istore.New(config)
	if err != nil {
		t.Fatalf("New(%v) failed: %v", config, err)
	}

	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}

	name := fmt.Sprintf("test/%x", buf)
	t.Logf("Storage server at %v", name)

	// Register the services.
	storeDispatcher := istore.NewStoreDispatcher(storeService, nil)
	if err := server.Register(name, storeDispatcher); err != nil {
		t.Fatalf("Register(%v) failed: %v", storeDispatcher, err)
	}

	// Create an endpoint and start listening.
	protocol, hostname := "tcp", "127.0.0.1:0"
	ep, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}

	name = naming.JoinAddressName(ep.String(), name)
	name = naming.MakeTerminal(name)

	// Create a closure that cleans things up.
	cleanup := func() {
		server.Stop()
		os.RemoveAll(dbName)
	}

	return name, cleanup
}
