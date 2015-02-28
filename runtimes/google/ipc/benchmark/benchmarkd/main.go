// A simple command-line tool to run the benchmark server.
package main

import (
	"v.io/v23"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/profiles/roaming"
	"v.io/x/ref/runtimes/google/ipc/benchmark/internal"
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	addr, stop := internal.StartServer(ctx, v23.GetListenSpec(ctx))
	vlog.Infof("Listening on %s", addr)
	defer stop()
	<-signals.ShutdownOnSignals(ctx)
}
