package main

import (
	"flag"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	vflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron/services/mgmt/profile/impl"
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
	runtime := rt.Init()
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
