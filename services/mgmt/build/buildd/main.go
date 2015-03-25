// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"os"

	"v.io/v23"
	"v.io/v23/services/mgmt/build"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/profiles/roaming"
	vflag "v.io/x/ref/security/flag"
	"v.io/x/ref/services/mgmt/build/impl"
)

var (
	gobin  = flag.String("gobin", "go", "path to the Go compiler")
	goroot = flag.String("goroot", os.Getenv("GOROOT"), "GOROOT to use with the Go compiler")
	name   = flag.String("name", "", "name to mount the build server as")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	ls := v23.GetListenSpec(ctx)
	endpoint, err := server.Listen(ls)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", ls, err)
		return
	}
	if err := server.Serve(*name, build.BuilderServer(impl.NewBuilderService(*gobin, *goroot)), vflag.NewAuthorizerOrDie()); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", *name, err)
		return
	}
	vlog.Infof("Build server running at endpoint=%q", endpoint)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
}
