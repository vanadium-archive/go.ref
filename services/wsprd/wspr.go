package main

import (
	"flag"

	"v.io/veyron/veyron2/rt"

	"v.io/veyron/veyron/lib/signals"
	// TODO(cnicolaou,benj): figure out how to support roaming as a chrome plugin
	"v.io/veyron/veyron/profiles/roaming"
	"v.io/wspr/veyron/services/wsprd/wspr"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on.")
	identd := flag.String("identd", "", "name of identd server.")

	flag.Parse()

	r, err := rt.New()
	if err != nil {
		panic("Could not initialize runtime: " + err.Error())
	}
	defer r.Cleanup()

	proxy := wspr.NewWSPR(r, *port, roaming.New, &roaming.ListenSpec, *identd, nil)
	defer proxy.Shutdown()

	proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	<-signals.ShutdownOnSignals(r)
}
