package main

import (
	"veyron/lib/signals"

	"veyron/services/mgmt/root/impl"
	"veyron2/rt"
	"veyron2/vlog"
)

func main() {
	r := rt.Init()
	defer r.Shutdown()
	server, err := r.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	suffix, dispatcher := "", impl.NewDispatcher()
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Errorf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
		return
	}
	protocol, hostname := "tcp", "localhost:0"
	ep, err := server.Listen(protocol, hostname)
	if err != nil {
		vlog.Errorf("Listen(%v, %v) failed: %v", protocol, hostname, err)
		return
	}
	vlog.VI(0).Infof("Listening on %v", ep)
	name := ""
	if err := server.Publish(name); err != nil {
		vlog.Errorf("Publish(%v) failed: %v", name, err)
		return
	}

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
