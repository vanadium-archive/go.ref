package main

import (
	"flag"
	"os"

	"veyron/lib/exec"
	"veyron/lib/signals"
	vflag "veyron/security/flag"
	"veyron/services/mgmt/node"
	"veyron/services/mgmt/node/impl"

	"veyron2/mgmt"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/vlog"
)

func main() {
	// TODO(rthellend): Remove the address and protocol flags when the config manager is working.
	var address, protocol, publishAs string
	flag.StringVar(&address, "address", "localhost:0", "network address to listen on")
	flag.StringVar(&protocol, "protocol", "tcp", "network type to listen on")
	flag.StringVar(&publishAs, "name", "", "name to publish the node manager at")
	flag.Parse()
	if os.Getenv(impl.OriginEnv) == "" {
		vlog.Fatalf("Specify the node manager origin as environment variable %s=<name>", impl.OriginEnv)
	}
	runtime := rt.Init()
	defer runtime.Shutdown()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()
	endpoint, err := server.Listen(protocol, address)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", protocol, address, err)
	}
	suffix, envelope := "", &application.Envelope{}
	name := naming.MakeTerminal(naming.JoinAddressName(endpoint.String(), suffix))
	vlog.VI(0).Infof("Node manager name: %v", name)
	// TODO(jsimsa): Replace <PreviousEnv> with a command-line flag when
	// command-line flags are supported in tests.
	dispatcher := impl.NewDispatcher(vflag.NewAuthorizerOrDie(), envelope, name, os.Getenv(impl.PreviousEnv))
	if err := server.Serve(publishAs, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishAs, err)
	}
	handle, _ := exec.GetChildHandle()
	if handle != nil {
		callbackName, err := handle.Config.Get(mgmt.ParentNodeManagerConfigKey)
		if err != nil {
			vlog.Fatalf("Couldn't get callback name from config: %v", err)
		}
		nmClient, err := node.BindConfig(callbackName)
		if err != nil {
			vlog.Fatalf("BindNode(%v) failed: %v", callbackName, err)
		}
		if err := nmClient.Set(rt.R().NewContext(), mgmt.ChildNodeManagerConfigKey, name); err != nil {
			vlog.Fatalf("Callback(%v, %v) failed: %v", mgmt.ChildNodeManagerConfigKey, name, err)
		}
	}
	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
