package main

import (
	"flag"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/services/mgmt/root/impl"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")
)

func main() {
	r := rt.Init()
	defer r.Cleanup()
	server, err := r.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	dispatcher := impl.NewDispatcher()
	ep, err := server.Listen(*protocol, *address)
	if err != nil {
		vlog.Errorf("Listen(%v, %v) failed: %v", *protocol, *address, err)
		return
	}
	vlog.VI(0).Infof("Listening on %v", ep)
	name := ""
	if err := server.Serve(name, dispatcher); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", name, err)
		return
	}

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
