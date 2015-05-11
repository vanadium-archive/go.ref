// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A simple command-line tool to run the benchmark server.
package main

import (
	"flag"
	"runtime"
	"time"

	"v.io/v23"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/runtime/factories/static"
	"v.io/x/ref/runtime/internal/rpc/stress/internal"
)

var (
	duration = flag.Duration("duration", 0, "duration of the stress test to run; if zero, there is no limit.")
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	ctx, shutdown := v23.Init()
	defer shutdown()

	server, ep, stop := internal.StartServer(ctx, v23.GetListenSpec(ctx))
	vlog.Infof("listening on %s", ep.Name())

	var timeout <-chan time.Time
	if *duration > 0 {
		timeout = time.After(*duration)
	}
	select {
	case <-timeout:
	case <-stop:
	case <-signals.ShutdownOnSignals(ctx):
	}

	if err := server.Stop(); err != nil {
		vlog.Fatalf("Stop() failed: %v", err)
	}
	vlog.Info("stopped.")
}
