// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Daemon wsprd implements the wspr web socket proxy as a stand-alone server.
package main

import (
	"flag"
	"fmt"
	"net"

	"v.io/v23"

	"v.io/x/ref/lib/signals"
	// TODO(cnicolaou,benj): figure out how to support roaming as a chrome plugin
	_ "v.io/x/ref/profiles/roaming"
	"v.io/x/ref/services/wspr/wsprlib"
)

func main() {
	port := flag.Int("port", 8124, "Port to listen on.")
	identd := flag.String("identd", "", "name of identd server.")

	flag.Parse()

	ctx, shutdown := v23.Init()
	defer shutdown()

	listenSpec := v23.GetListenSpec(ctx)
	proxy := wsprlib.NewWSPR(ctx, *port, &listenSpec, *identd, nil)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	nhost, nport, _ := net.SplitHostPort(addr.String())
	fmt.Printf("Listening on host: %s port: %s\n", nhost, nport)
	<-signals.ShutdownOnSignals(ctx)
}
