// a simple command-line tool to run the benchmark server.
package main

import (
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	"veyron.io/veyron/veyron/runtimes/google/ipc/benchmarks"
)

func main() {
	r, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %s", err)
	}
	defer r.Cleanup()

	addr, stop := benchmarks.StartServer(r, roaming.ListenSpec)
	vlog.Infof("Listening on %s", addr)
	defer stop()
	<-signals.ShutdownOnSignals(r)
}
