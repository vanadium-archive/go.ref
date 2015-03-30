// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"v.io/v23"

	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/profiles/static"
	irole "v.io/x/ref/services/security/roled/internal"
)

var (
	configDir = flag.String("config_dir", "", "The directory where the role configuration files are stored.")
	name      = flag.String("name", "", "The name to publish for this service.")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	if len(*configDir) == 0 {
		fmt.Fprintf(os.Stderr, "--config_dir must be specified\n")
		os.Exit(1)
	}
	if len(*name) == 0 {
		fmt.Fprintf(os.Stderr, "--name must be specified\n")
		os.Exit(1)
	}
	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}

	listenSpec := v23.GetListenSpec(ctx)
	eps, err := server.Listen(listenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%v) failed: %v", listenSpec, err)
	}
	vlog.Infof("Listening on: %q", eps)
	if err := server.ServeDispatcher(*name, irole.NewDispatcher(*configDir, *name)); err != nil {
		vlog.Fatalf("ServeDispatcher(%q) failed: %v", *name, err)
	}
	if len(*name) > 0 {
		fmt.Printf("NAME=%s\n", *name)
	} else if len(eps) > 0 {
		fmt.Printf("NAME=%s\n", eps[0].Name())
	}
	<-signals.ShutdownOnSignals(ctx)
}
