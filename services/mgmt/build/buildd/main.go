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

func main() {
	var address, gobin, name, protocol string
	// TODO(rthellend): Remove the address and protocol flags when the config manager is working.
	flag.StringVar(&address, "address", "localhost:0", "network address to listen on")
	flag.StringVar(&gobin, "gobin", "go", "path to the Go compiler")
	flag.StringVar(&name, "name", "", "name to mount the build server as")
	flag.StringVar(&protocol, "protocol", "tcp", "network type to listen on")
	flag.Parse()
	runtime := rt.Init()
	defer runtime.Cleanup()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	endpoint, err := server.Listen(protocol, address)
	if err != nil {
		vlog.Errorf("Listen(%v, %v) failed: %v", protocol, address, err)
		return
	}
	if err := server.Serve(name, ipc.SoloDispatcher(build.NewServerBuild(impl.NewInvoker(gobin)), vflag.NewAuthorizerOrDie())); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", name, err)
		return
	}
	vlog.Infof("Build server endpoint=%q name=%q", endpoint, name)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
