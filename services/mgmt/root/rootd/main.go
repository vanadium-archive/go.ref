package main

import (
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	"veyron.io/veyron/veyron/services/mgmt/root/impl"
)

func main() {
	r, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %v", err)
	}
	defer r.Cleanup()
	server, err := r.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	dispatcher := impl.NewDispatcher()
	ep, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", roaming.ListenSpec, err)
		return
	}
	vlog.VI(0).Infof("Listening on %v", ep)
	name := ""
	if err := server.ServeDispatcher(name, dispatcher); err != nil {
		vlog.Errorf("ServeDispatcher(%v) failed: %v", name, err)
		return
	}

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(r)
}
