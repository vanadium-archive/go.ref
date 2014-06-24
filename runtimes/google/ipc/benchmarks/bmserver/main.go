// a simple command-line tool to run the benchmark server.
package main

import (
	"flag"

	"veyron/lib/signals"
	"veyron/runtimes/google/ipc/benchmarks"

	"veyron2/rt"
	"veyron2/vlog"
)

var (
	address  = flag.String("address", ":0", "address to listen on")
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
)

func main() {
	rt.Init()
	addr, stop := benchmarks.StartServer(*protocol, *address)
	vlog.Infof("Listening on %s", addr)
	defer stop()
	<-signals.ShutdownOnSignals()
}
