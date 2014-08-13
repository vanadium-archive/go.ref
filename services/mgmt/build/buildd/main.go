package main

import (
	"flag"

	"veyron/lib/signals"
	vflag "veyron/security/flag"
	"veyron/services/mgmt/build/impl"

	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/services/mgmt/build"
	"veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

	gobin = flag.String("gobin", "go", "path to the Go compiler")
	name  = flag.String("name", "", "name to mount the build server as")
)

func main() {
	flag.Parse()
	runtime := rt.Init()
	defer runtime.Cleanup()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	endpoint, err := server.Listen(*protocol, *address)
	if err != nil {
		vlog.Errorf("Listen(%v, %v) failed: %v", *protocol, *address, err)
		return
	}
	if err := server.Serve(*name, ipc.SoloDispatcher(build.NewServerBuilder(impl.NewInvoker(*gobin)), vflag.NewAuthorizerOrDie())); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", *name, err)
		return
	}
	vlog.Infof("Build server endpoint=%q name=%q", endpoint, *name)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
