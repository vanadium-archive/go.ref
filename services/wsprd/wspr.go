package main

import (
	"flag"

	"veyron.io/veyron/veyron/lib/signals"
	// TODO(cnicolaou,benj): figure out how to support roaming as a chrome plugin
	"veyron.io/veyron/veyron/profiles/roaming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/wspr/veyron/services/wsprd/wspr"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on.")
	identd := flag.String("identd", "", "identd server name. Must be set.")

	flag.Parse()

	rt.Init()

	proxy := wspr.NewWSPR(*port, roaming.ListenSpec, *identd)
	defer proxy.Shutdown()

	proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	<-signals.ShutdownOnSignals()
}
