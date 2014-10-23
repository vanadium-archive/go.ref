package main

import (
	"flag"

	"veyron.io/veyron/veyron/lib/signals"
	// TODO(cnicolaou,benj): figure out how to support roaming as a chrome plugi
	"veyron.io/veyron/veyron/profiles/roaming"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/wspr/veyron/services/wsprd/wspr"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on.")
	identd := flag.String("identd", "", "The endpoint for the identd server.  This must be set.")
	// TODO(ataly, ashankar, bjornick): Remove this flag once the old security
	// model is killed.
	newSecurityModel := flag.Bool("new_security_model", false, "Enable the new security model")
	flag.Parse()

	rt.Init()

	var opts []veyron2.ROpt
	if *newSecurityModel {
		opts = append(opts, options.ForceNewSecurityModel{})
	}

	proxy := wspr.NewWSPR(*port, *roaming.ListenSpec, *identd, opts...)
	defer proxy.Shutdown()
	go func() {
		proxy.Run()
	}()

	<-signals.ShutdownOnSignals()
}
