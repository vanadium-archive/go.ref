package main

import (
	"flag"

	"veyron/lib/signals"
	vflag "veyron/security/flag"
	"veyron/services/mgmt/node/config"
	"veyron/services/mgmt/node/impl"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

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
	endpoint, err := server.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", *protocol, *address, err)
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
	dispatcher, err := impl.NewDispatcher(vflag.NewAuthorizerOrDie(), configState)
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
