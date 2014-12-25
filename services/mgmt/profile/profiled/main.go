package main

import (
	"flag"

	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/vlog"

	"v.io/veyron/veyron/lib/signals"
	"v.io/veyron/veyron/profiles/roaming"
	vflag "v.io/veyron/veyron/security/flag"
	"v.io/veyron/veyron/services/mgmt/profile/impl"
)

var (
	name  = flag.String("name", "", "name to mount the profile repository as")
	store = flag.String("store", "", "local directory to store profiles in")
)

func main() {
	flag.Parse()
	if *store == "" {
		vlog.Fatalf("Specify a directory for storing profiles using --store=<name>")
	}
	runtime, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %v", err)
	}
	defer runtime.Cleanup()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dispatcher, err := impl.NewDispatcher(*store, vflag.NewAuthorizerOrDie())
	if err != nil {
		vlog.Fatalf("NewDispatcher() failed: %v", err)
	}

	endpoint, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", roaming.ListenSpec, err)
	}
	if err := server.ServeDispatcher(*name, dispatcher); err != nil {
		vlog.Fatalf("ServeDispatcher(%v) failed: %v", *name, err)
	}
	vlog.Infof("Profile repository running at endpoint=%q", endpoint)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(runtime)
}
