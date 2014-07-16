package main

import (
	"flag"

	"veyron/lib/signals"
	"veyron/services/wsprd/wspr"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on")
	veyronProxy := flag.String("vproxy", "", "The endpoint for the veyron proxy to publish on. This must be set")
	flag.Parse()

	proxy := wspr.NewWSPR(*port, *veyronProxy)
	defer proxy.Shutdown()
	go func() {
		proxy.Run()
	}()

	<-signals.ShutdownOnSignals()
}
