package main

import (
	"flag"

	"veyron.io/veyron/veyron/lib/signals"
	vflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron/services/mgmt/application/impl"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

	name  = flag.String("name", "", "name to mount the application repository as")
	store = flag.String("store", "", "local directory to store application envelopes in")
)

func main() {
	flag.Parse()
	if *store == "" {
		vlog.Fatalf("Specify a directory for storing application envelopes using --store=<name>")
	}
	runtime := rt.Init()
	defer runtime.Cleanup()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dispatcher, err := impl.NewDispatcher(*store, vflag.NewAuthorizerOrDie())
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
	vlog.Infof("Application repository running at endpoint=%q", endpoint)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
