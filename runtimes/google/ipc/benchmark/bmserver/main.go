// A simple command-line tool to run the benchmark server.
package main

import (
	"v.io/core/veyron/lib/signals"
	_ "v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/runtimes/google/ipc/benchmark"

	"v.io/core/veyron2"
	"v.io/core/veyron2/vlog"
)

func main() {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	addr, stop := benchmark.StartServer(ctx, veyron2.GetListenSpec(ctx))
	vlog.Infof("Listening on %s", addr)
	defer stop()
	<-signals.ShutdownOnSignals(ctx)
}
