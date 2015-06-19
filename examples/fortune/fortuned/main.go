// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command fortuned runs a daemon that implements the Fortune interface.
package main

import (
	"flag"
	"log"

	"v.io/v23"
	"v.io/v23/security"
	"v.io/x/ref/examples/fortune"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/xrpc"
	// The v23.Init call below will use the generic runtime factory.
	_ "v.io/x/ref/runtime/factories/generic"
)

var (
	name = flag.String("name", "", "Name for fortuned in default mount table")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	authorizer := security.DefaultAuthorizer()
	impl := newImpl()
	service := fortune.FortuneServer(impl)

	server, err := xrpc.NewServer(ctx, *name, service, authorizer)
	if err != nil {
		log.Panic("Failure creating server: ", err)
	}
	log.Printf("Listening at: %v\n", server.Status().Endpoints[0])

	<-signals.ShutdownOnSignals(ctx)
}
