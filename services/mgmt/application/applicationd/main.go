package main

import (
	"flag"

	"v.io/v23"
	"v.io/v23/vlog"

	"v.io/core/veyron/lib/signals"
	_ "v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/services/mgmt/application/impl"
)

var (
	name  = flag.String("name", "", "name to mount the application repository as")
	store = flag.String("store", "", "local directory to store application envelopes in")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	if *store == "" {
		vlog.Fatalf("Specify a directory for storing application envelopes using --store=<name>")
	}

	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dispatcher, err := impl.NewDispatcher(*store)
	if err != nil {
		vlog.Fatalf("NewDispatcher() failed: %v", err)
	}

	ls := v23.GetListenSpec(ctx)
	endpoints, err := server.Listen(ls)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", ls, err)
	}
	if err := server.ServeDispatcher(*name, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", *name, err)
	}
	epName := endpoints[0].Name()
	if *name != "" {
		vlog.Infof("Application repository serving at %q (%q)", *name, epName)
	} else {
		vlog.Infof("Application repository serving at %q", epName)
	}
	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
}
