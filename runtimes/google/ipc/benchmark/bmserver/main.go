// A simple command-line tool to run the benchmark server.
package main

import (
	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/runtimes/google/ipc/benchmark"

	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"
)

func main() {
	vrt, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %s", err)
	}
	defer vrt.Cleanup()

	addr, stop := benchmark.StartServer(vrt, roaming.ListenSpec)
	vlog.Infof("Listening on %s", addr)
	defer stop()
	<-signals.ShutdownOnSignals(vrt)
}
