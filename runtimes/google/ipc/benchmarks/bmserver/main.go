// a simple command-line tool to run the benchmark server.
package main

import (
	"flag"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/runtimes/google/ipc/benchmarks"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")
)

func main() {
	r := rt.Init()
	addr, stop := benchmarks.StartServer(r, *protocol, *address)
	vlog.Infof("Listening on %s", addr)
	defer stop()
	<-signals.ShutdownOnSignals()
}
