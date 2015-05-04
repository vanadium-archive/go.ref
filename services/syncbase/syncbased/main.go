// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// syncbased is a syncbase daemon.
package main

// Example invocation:
// syncbased --veyron.tcp.address="127.0.0.1:0" --name=syncbased

import (
	"flag"

	"v.io/v23"
	"v.io/v23/security/access"
	"v.io/x/lib/vlog"

	"v.io/syncbase/x/ref/services/syncbase/server"
	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/profiles/roaming"
)

// TODO(sadovsky): Perhaps this should be one of the standard Veyron flags.
var (
	name = flag.String("name", "", "Name to mount at.")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	s, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatal("v23.NewServer() failed: ", err)
	}
	if _, err := s.Listen(v23.GetListenSpec(ctx)); err != nil {
		vlog.Fatal("s.Listen() failed: ", err)
	}

	// TODO(sadovsky): Use a real Permissions.
	service, err := server.NewService(nil, nil, access.Permissions{})
	if err != nil {
		vlog.Fatal("server.NewService() failed: ", err)
	}
	d := server.NewDispatcher(service)

	// Publish the service in the mount table.
	if err := s.ServeDispatcher(*name, d); err != nil {
		vlog.Fatal("s.ServeDispatcher() failed: ", err)
	}
	vlog.Info("Mounted at: ", *name)

	// Wait forever.
	<-signals.ShutdownOnSignals(ctx)
}
