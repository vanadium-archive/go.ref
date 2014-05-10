package main

import (
	"flag"

	"veyron/lib/signals"
	"veyron/services/wspr/wsprd/lib"
	"veyron2/services/mounttable"
	"veyron2/vom"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on")
	veyronProxy := flag.String("vproxy", "", "The endpoint for the veyron proxy to publish on. This must be set")
	flag.Parse()

	// TODO(bprosnitz) Remove this once VOM2 goes in.
	vom.Register(mounttable.MountEntry{})

	proxy := lib.NewWSPR(*port, *veyronProxy)
	defer proxy.Shutdown()
	go func() {
		proxy.Run()
	}()

	<-signals.ShutdownOnSignals()
}
