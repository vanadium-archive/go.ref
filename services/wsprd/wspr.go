package main

import (
	"flag"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/services/wsprd/wspr"
	"veyron.io/veyron/veyron2/rt"
	// TODO(cnicolaou,benj): figure out how to support roaming as a chrome plugi
	"veyron.io/veyron/veyron/profiles/roaming"
)

func main() {
	identd := flag.String("identd", "", "The endpoint for the identd server.  This must be set.")
	flag.Parse()

	rt.Init()

	proxy := wspr.NewWSPR(*roaming.ListenSpec, *identd)
	defer proxy.Shutdown()
	go func() {
		proxy.Run()
	}()

	<-signals.ShutdownOnSignals()
}
