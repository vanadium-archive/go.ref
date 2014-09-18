package main

import (
	"flag"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/services/wsprd/wspr"
	"veyron.io/veyron/veyron2/rt"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on.")
	veyronProxy := flag.String("vproxy", "", "The endpoint for the veyron proxy to publish on. This must be set.")
	identd := flag.String("identd", "", "The endpoint for the identd server.  This must be set.")
	flag.Parse()

	rt.Init()

	proxy := wspr.NewWSPR(*port, *veyronProxy, *identd)
	defer proxy.Shutdown()
	go func() {
		proxy.Run()
	}()

	<-signals.ShutdownOnSignals()
}
