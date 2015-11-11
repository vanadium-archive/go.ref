// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

package main

// To build:
// cd $JIRI_ROOT/release/projects/mojo/syncbase
// make build

import (
	"log"
	"os"

	"mojo/public/go/application"
	"mojo/public/go/bindings"
	"mojo/public/go/system"

	"mojom/syncbase"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/ref/services/syncbase/server"
)

//#include "mojo/public/c/system/types.h"
import "C"

type delegate struct {
	ctx      *context.T
	disp     rpc.Dispatcher
	shutdown func()
	srv      rpc.Server
	cleanup  func()
	stubs    []*bindings.Stub
}

func (d *delegate) Initialize(actx application.Context) {
	// actx.Args() is a slice that contains the url of this mojo service
	// followed by all arguments passed to the mojo service via the
	// "--args-for" flag.
	// Since the v23 runtime factories parse arguments from os.Args, we must
	// overwrite os.Args with actx.Args().
	// Note that os.Args must be set before calling v23.Init().
	os.Args = actx.Args()
	d.ctx, d.shutdown = v23.Init()
	if err := setBlessings(d.ctx, actx); err != nil {
		panic(err)
	}
	d.srv, d.disp, d.cleanup = Serve(d.ctx)
}

func (d *delegate) Create(req syncbase.Syncbase_Request) {
	impl := server.NewMojoImpl(d.ctx, d.srv, d.disp)
	stub := syncbase.NewSyncbaseStub(req, impl, bindings.GetAsyncWaiter())
	d.stubs = append(d.stubs, stub)
	go func() {
		for {
			if err := stub.ServeRequest(); err != nil {
				connErr, ok := err.(*bindings.ConnectionError)
				if !ok || !connErr.Closed() {
					log.Println(err)
				}
				break
			}
		}
	}()
}

func (d *delegate) AcceptConnection(conn *application.Connection) {
	conn.ProvideServices(&syncbase.Syncbase_ServiceFactory{d})
}

func (d *delegate) Quit() {
	for _, stub := range d.stubs {
		stub.Close()
	}
	d.cleanup()
	d.shutdown()
}

//export MojoMain
func MojoMain(handle C.MojoHandle) C.MojoResult {
	application.Run(&delegate{}, system.MojoHandle(handle))
	return C.MOJO_RESULT_OK
}

// NOTE(nlacasse): Mojo runs Go code by calling MojoMain().  The main() method
// below is still needed because the Go tool won't build without it.
func main() {}
