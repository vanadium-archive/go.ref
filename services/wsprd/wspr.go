package main

import (
	"flag"
	"fmt"
	"net"

	"v.io/core/veyron2"

	"v.io/core/veyron/lib/signals"
	// TODO(cnicolaou,benj): figure out how to support roaming as a chrome plugin
	_ "v.io/core/veyron/profiles/roaming"
	"v.io/wspr/veyron/services/wsprd/wspr"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on.")
	identd := flag.String("identd", "", "name of identd server.")

	flag.Parse()

	ctx, shutdown := veyron2.Init()
	defer shutdown()

	listenSpec := veyron2.GetListenSpec(ctx)
	proxy := wspr.NewWSPR(ctx, *port, &listenSpec, *identd, nil)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	nhost, nport, _ := net.SplitHostPort(addr.String())
	fmt.Printf("Listening on host: %s port: %s\n", nhost, nport)
	<-signals.ShutdownOnSignals(ctx)
}
