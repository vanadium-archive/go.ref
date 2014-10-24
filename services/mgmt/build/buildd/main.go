package main

import (
	"flag"
	"os"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/build"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	vflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron/services/mgmt/build/impl"
)

var (
	gobin  = flag.String("gobin", "go", "path to the Go compiler")
	goroot = flag.String("goroot", os.Getenv("GOROOT"), "GOROOT to use with the Go compiler")
	name   = flag.String("name", "", "name to mount the build server as")
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
	endpoint, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", roaming.ListenSpec, err)
		return
	}
	if err := server.Serve(*name, ipc.LeafDispatcher(build.NewServerBuilder(impl.NewInvoker(*gobin, *goroot)), vflag.NewAuthorizerOrDie())); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", *name, err)
		return
	}
	vlog.Infof("Build server running at endpoint=%q", endpoint)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
