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
