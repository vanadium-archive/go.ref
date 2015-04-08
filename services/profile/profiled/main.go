// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"

	"v.io/v23"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/signals"

	_ "v.io/x/ref/profiles/roaming"
)

var (
	name  = flag.String("name", "", "name to mount the profile repository as")
	store = flag.String("store", "", "local directory to store profiles in")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	if *store == "" {
		vlog.Fatalf("Specify a directory for storing profiles using --store=<name>")
	}

	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}

	dispatcher, err := NewDispatcher(*store, securityflag.NewAuthorizerOrDie())
	if err != nil {
		vlog.Fatalf("NewDispatcher() failed: %v", err)
	}

	ls := v23.GetListenSpec(ctx)
	endpoint, err := server.Listen(ls)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", ls, err)
	}
	if err := server.ServeDispatcher(*name, dispatcher); err != nil {
		vlog.Fatalf("ServeDispatcher(%v) failed: %v", *name, err)
	}
	vlog.Infof("Profile repository running at endpoint=%q", endpoint)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
}
