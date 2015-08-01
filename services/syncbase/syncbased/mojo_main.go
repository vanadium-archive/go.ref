// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

package main

// To build:
// cd $V23_ROOT/experimental/projects/ether
// make gen/mojo/syncbased.mojo

// TODO(sadovsky): Currently fails with error "flag provided but not defined:
// -child-connection-id". Need to debug. Probably just need to peel off any Mojo
// flags before having the Syncbase code do its flag parsing.

import (
	"log"

	"golang.org/x/mobile/app"

	"mojo/public/go/application"
	"mojo/public/go/bindings"
	"mojo/public/go/system"

	"mojom/syncbase"

	"v.io/syncbase/x/ref/services/syncbase/server"
	"v.io/v23"
)

//#include "mojo/public/c/system/types.h"
import "C"

type delegate struct {
	service interface{} // actual type is *server.service
	stubs   []*bindings.Stub
}

func (d *delegate) Initialize(ctx application.Context) {}

func (d *delegate) Create(req syncbase.Syncbase_Request) {
	impl := server.NewMojoImpl(d.service)
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
}

//export MojoMain
func MojoMain(handle C.MojoHandle) C.MojoResult {
	ctx, shutdown := v23.Init()
	defer shutdown()
	application.Run(&delegate{service: Serve(ctx)}, system.MojoHandle(handle))
	return C.MOJO_RESULT_OK
}

func main() {
	app.Run(app.Callbacks{})
}
