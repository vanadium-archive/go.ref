package main

import (
	"flag"

	"veyron/lib/signals"
	vflag "veyron/security/flag"

	"veyron/services/mgmt/application/impl"
	"veyron2/rt"
	"veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

	name      = flag.String("name", "", "name to mount the application manager as")
	storeName = flag.String("store", "", "object name of the application manager store")
)

func main() {
	flag.Parse()
	if *storeName == "" {
		vlog.Fatalf("Specify a store using --store=<name>")
	}
	runtime := rt.Init()
	defer runtime.Cleanup()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dispatcher, err := impl.NewDispatcher(*storeName, vflag.NewAuthorizerOrDie())
	if err != nil {
		vlog.Fatalf("NewDispatcher() failed: %v", err)
	}
	endpoint, err := server.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", *protocol, *address, err)
	}
	if err := server.Serve(*name, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", *name, err)
	}
	vlog.VI(0).Infof("Application repository published at %v/%v", endpoint, *name)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
