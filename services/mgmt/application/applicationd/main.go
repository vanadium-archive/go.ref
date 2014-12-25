package main

import (
	"flag"

	"v.io/veyron/veyron2/naming"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/vlog"

	"v.io/veyron/veyron/lib/signals"
	"v.io/veyron/veyron/profiles/roaming"
	vflag "v.io/veyron/veyron/security/flag"
	"v.io/veyron/veyron/services/mgmt/application/impl"
)

var (
	name  = flag.String("name", "", "name to mount the application repository as")
	store = flag.String("store", "", "local directory to store application envelopes in")
)

func main() {
	flag.Parse()
	if *store == "" {
		vlog.Fatalf("Specify a directory for storing application envelopes using --store=<name>")
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

	endpoints, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", roaming.ListenSpec, err)
	}
	if err := server.ServeDispatcher(*name, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", *name, err)
	}
	epName := naming.JoinAddressName(endpoints[0].String(), "")
	if *name != "" {
		vlog.Infof("Application repository serving at %q (%q)", *name, epName)
	} else {
		vlog.Infof("Application repository serving at %q", epName)
	}
	// Wait until shutdown.
	<-signals.ShutdownOnSignals(runtime)
}
