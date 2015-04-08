// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command tunneld is an implementation of the tunnel service.
package main

import (
	"flag"
	"fmt"

	"v.io/v23"
	"v.io/x/lib/vlog"
	"v.io/x/ref/examples/tunnel"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/signals"

	_ "v.io/x/ref/profiles/roaming"
)

var (
	name = flag.String("name", "", "name at which to publish the server")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	auth := securityflag.NewAuthorizerOrDie()
	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	listenSpec := v23.GetListenSpec(ctx)
	if _, err := server.Listen(listenSpec); err != nil {
		vlog.Fatalf("Listen(%v) failed: %v", listenSpec, err)
	}
	if err := server.Serve(*name, tunnel.TunnelServer(&T{}), auth); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", *name, err)
	}
	status := server.Status()
	vlog.Infof("Listening on: %v", status.Endpoints)
	if len(status.Endpoints) > 0 {
		fmt.Printf("NAME=%s\n", status.Endpoints[0].Name())
	}
	vlog.Infof("Published as %q", *name)

	<-signals.ShutdownOnSignals(ctx)
}
