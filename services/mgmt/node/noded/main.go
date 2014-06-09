package main

import (
	"flag"
	"os"

	"veyron/lib/exec"
	"veyron/lib/signals"
	vflag "veyron/security/flag"
	"veyron/services/mgmt/node"
	"veyron/services/mgmt/node/impl"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/vlog"
)

func main() {
	// TODO(rthellend): Remove the address and protocol flags when the config manager is working.
	var address, protocol, publishAs string
	flag.StringVar(&address, "address", "localhost:0", "network address to listen on")
	flag.StringVar(&publishAs, "name", "", "name to publish the node manager at")
	flag.StringVar(&protocol, "protocol", "tcp", "network type to listen on")
	flag.Parse()
	if os.Getenv(impl.ORIGIN_ENV) == "" {
		vlog.Fatalf("Specify the node manager origin as environment variable %s=<name>", impl.ORIGIN_ENV)
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
	envelope := &application.Envelope{}
	suffix, crSuffix, arSuffix := "", "cr", "ar"
	name := naming.MakeTerminal(naming.JoinAddressName(endpoint.String(), suffix))
	vlog.VI(0).Infof("Node manager name: %v", name)
	dispatcher, crDispatcher, arDispatcher := impl.NewDispatchers(vflag.NewAuthorizerOrDie(), envelope, name)
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	if err := server.Register(crSuffix, crDispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", crSuffix, crDispatcher, err)
	}
	if err := server.Register(arSuffix, arDispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", arSuffix, arDispatcher, err)
	}
	if len(publishAs) > 0 {
		if err := server.Publish(publishAs); err != nil {
			vlog.Fatalf("Publish(%v) failed: %v", publishAs, err)
		}
	}
	handle, err := exec.GetChildHandle()
	if handle != nil && handle.CallbackName != "" {
		nmClient, err := node.BindCallbackReceiver(handle.CallbackName)
		if err != nil {
			vlog.Fatalf("BindNode(%v) failed: %v", handle.CallbackName, err)
		}
		if err := nmClient.Callback(rt.R().NewContext(), name); err != nil {
			vlog.Fatalf("Callback(%v) failed: %v", name, err)
		}
	}
	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
