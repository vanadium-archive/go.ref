package main

import (
	"flag"

	"veyron/lib/signals"
	"veyron/runtimes/google/lib/exec"
	"veyron/services/mgmt/node/impl"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/vlog"
)

func main() {
	var name, origin string
	flag.StringVar(&name, "name", "", "name to publish the node manager at")
	flag.StringVar(&origin, "origin", "", "node manager application repository")
	flag.Parse()
	if origin == "" {
		vlog.Fatalf("Specify an origin using --origin=<name>")
	}
	runtime := rt.Init()
	defer runtime.Shutdown()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()
	envelope := &application.Envelope{}
	dispatcher := impl.NewDispatcher(envelope, origin)
	suffix := ""
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	vlog.VI(0).Infof("Listening on %v", endpoint)
	if len(name) > 0 {
		if err := server.Publish(name); err != nil {
			vlog.Fatalf("Publish(%v) failed: %v", name, err)
		}
	}
	handle, err := exec.NewChildHandle()
	switch err {
	case nil:
		// Node manager was started by self-update, notify the parent
		// process that you are ready.
		handle.SetReady()
	case exec.ErrNoVersion:
		// Node manager was not started by self-update, no action is
		// needed.
	default:
		vlog.Fatalf("NewChildHandle() failed: %v", err)
	}
	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
