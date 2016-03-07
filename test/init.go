// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !mojo

package test

import (
	"flag"

	"os"
	"os/signal"
	"time"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/ref/services/debug/debug/browseserver"
)

var debugOnShutdown = flag.String("v23.debug-address", "",
	"If this is set then when a test runs, we start a debug browser (serving at the given address)"+
		" and hang so you can look at it. This only works when running a single test,"+
		" because you have to ctrl-c to finish the test.")

// V23Init initializes the runtime and sets up the principal with a self-signed
// TestBlessing. The blessing setup step is skipped if this function is invoked
// from a v23test.Shell child process, since v23test.Shell passes credentials to
// its children.
// NOTE: For tests involving Vanadium RPCs, developers are encouraged to use
// V23InitWithMounttable, and have their services access each other via the
// mount table (rather than using endpoint strings).
func V23Init() (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init()
	ctx = internalInit(ctx, false)

	if *debugOnShutdown != "" {
		orig := shutdown
		shutdown = func() { debug(ctx, orig) }
	}

	return ctx, shutdown
}

// V23InitWithMounttable initializes the runtime and:
// - Sets up the principal with a self-signed TestBlessing
// - Starts a mounttable and sets the namespace roots appropriately
// Both these steps are skipped if this function is invoked from a v23test.Shell
// child process.
func V23InitWithMounttable() (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init()
	ctx = internalInit(ctx, true)

	if *debugOnShutdown != "" {
		orig := shutdown
		shutdown = func() { debug(ctx, orig) }
	}

	return ctx, shutdown
}

func debug(ctx *context.T, shutdown func()) {
	ctx, cancel := context.WithCancel(ctx)
	ctx, s, err := v23.WithNewServer(ctx, "", &dummyService{}, security.AllowEveryone())
	if err != nil {
		panic(err)
	}
	eps := s.Status().Endpoints

	go func() {
		ch := make(chan os.Signal)
		signal.Notify(ch, os.Interrupt)
		<-ch
		signal.Stop(ch)
		cancel()
	}()

	if err := browseserver.Serve(ctx, *debugOnShutdown, eps[0].Name(), 10*time.Second, false); err != nil {
		ctx.Infof("Stopped debug server: %v", err)
	}
	shutdown()
}

type dummyService struct{}

func (*dummyService) Do(ctx *context.T, call rpc.ServerCall) error {
	return nil
}
