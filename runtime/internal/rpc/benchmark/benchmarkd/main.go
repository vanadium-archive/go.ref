// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A simple command-line tool to run the benchmark server.
package main

import (
	"v.io/v23"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/runtime/internal/rpc/benchmark/internal"
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	ep, stop := internal.StartServer(ctx, v23.GetListenSpec(ctx))
	vlog.Infof("Listening on %s", ep.Name())
	defer stop()
	<-signals.ShutdownOnSignals(ctx)
}
