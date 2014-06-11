package main

import (
	"flag"

	"veyron/lib/signals"
	"veyron/services/wspr/wsprd/lib"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on")
	veyronProxy := flag.String("vproxy", "", "The endpoint for the veyron proxy to publish on. This must be set")
	flag.Parse()

	proxy := lib.NewWSPR(*port, *veyronProxy)
	defer proxy.Shutdown()
	go func() {
		proxy.Run()
	}()

	<-signals.ShutdownOnSignals()
}
