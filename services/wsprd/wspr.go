package main

import (
	"flag"

	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/signals"
	// TODO(cnicolaou,benj): figure out how to support roaming as a chrome plugin
	"veyron.io/veyron/veyron/profiles/roaming"
	"veyron.io/wspr/veyron/services/wsprd/wspr"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on.")
	identd := flag.String("identd", "", "identd server name. Must be set.")

	flag.Parse()

	// TODO(mattr): This runtime isn't really used and should be removed.
	// Unfortunately if you don't initialize it some parts of the ListenSpec
	// are not properly initialized.  We should fix that behavior as it's
	// very unintuitive.
	r, err := rt.New()
	if err != nil {
		panic("Could not initialize runtime: " + err.Error())
	}
	defer r.Cleanup()

	proxy := wspr.NewWSPR(*port, roaming.New, &roaming.ListenSpec, *identd, nil)
	defer proxy.Shutdown()

	proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	<-signals.ShutdownOnSignals(r)
}
