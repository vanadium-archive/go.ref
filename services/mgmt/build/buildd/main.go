package main

import (
	"flag"
	"os"

	"v.io/core/veyron2"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/services/mgmt/build"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/profiles/roaming"
	vflag "v.io/core/veyron/security/flag"
	"v.io/core/veyron/services/mgmt/build/impl"
)

var (
	gobin  = flag.String("gobin", "go", "path to the Go compiler")
	goroot = flag.String("goroot", os.Getenv("GOROOT"), "GOROOT to use with the Go compiler")
	name   = flag.String("name", "", "name to mount the build server as")
)

func main() {
	flag.Parse()
	runtime, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %v", err)
	}
	defer runtime.Cleanup()

	ctx := runtime.NewContext()

	server, err := veyron2.NewServer(ctx)
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
	if err := server.Serve(*name, build.BuilderServer(impl.NewBuilderService(*gobin, *goroot)), vflag.NewAuthorizerOrDie()); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", *name, err)
		return
	}
	vlog.Infof("Build server running at endpoint=%q", endpoint)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
}
