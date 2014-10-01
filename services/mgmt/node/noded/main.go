package main

import (
	"flag"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	"veyron.io/veyron/veyron/services/mgmt/node/config"
	"veyron.io/veyron/veyron/services/mgmt/node/impl"
)

var (
	publishAs = flag.String("name", "", "name to publish the node manager at")
)

func main() {
	flag.Parse()
	runtime := rt.Init()
	defer runtime.Cleanup()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()
	endpoint, err := server.ListenX(roaming.ListenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", roaming.ListenSpec, err)
	}
	name := naming.MakeTerminal(naming.JoinAddressName(endpoint.String(), ""))
	vlog.VI(0).Infof("Node manager object name: %v", name)
	configState, err := config.Load()
	if err != nil {
		vlog.Fatalf("Failed to load config passed from parent: %v", err)
		return
	}
	configState.Name = name
	// TODO(caprita): We need a way to set config fields outside of the
	// update mechanism (since that should ideally be an opaque
	// implementation detail).
	dispatcher, err := impl.NewDispatcher(configState)
	if err != nil {
		vlog.Fatalf("Failed to create dispatcher: %v", err)
	}
	if err := server.Serve(*publishAs, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", *publishAs, err)
	}
	impl.InvokeCallback(name)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
