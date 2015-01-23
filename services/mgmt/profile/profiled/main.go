package main

import (
	"flag"

	"v.io/core/veyron2"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	_ "v.io/core/veyron/profiles/roaming"
	vflag "v.io/core/veyron/security/flag"
	"v.io/core/veyron/services/mgmt/profile/impl"
)

var (
	name  = flag.String("name", "", "name to mount the profile repository as")
	store = flag.String("store", "", "local directory to store profiles in")
)

func main() {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	if *store == "" {
		vlog.Fatalf("Specify a directory for storing profiles using --store=<name>")
	}

	server, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}

	dispatcher, err := impl.NewDispatcher(*store, vflag.NewAuthorizerOrDie())
	if err != nil {
		vlog.Fatalf("NewDispatcher() failed: %v", err)
	}

	ls := veyron2.GetListenSpec(ctx)
	endpoint, err := server.Listen(ls)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", ls, err)
	}
	if err := server.ServeDispatcher(*name, dispatcher); err != nil {
		vlog.Fatalf("ServeDispatcher(%v) failed: %v", *name, err)
	}
	vlog.Infof("Profile repository running at endpoint=%q", endpoint)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
}
