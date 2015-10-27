// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/securityflag"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/syncbase/server"
)

var (
	name    = flag.String("name", "", "Name to mount at.")
	rootDir = flag.String("root-dir", "/var/lib/syncbase", "Root dir for storage engines and other data")
	engine  = flag.String("engine", "leveldb", "Storage engine to use. Currently supported: memstore and leveldb.")
)

// TODO(sadovsky): We return rpc.Server and rpc.Dispatcher as a quick hack to
// support Mojo.
func Serve(ctx *context.T) (rpc.Server, rpc.Dispatcher, func()) {
	perms, err := securityflag.PermissionsFromFlag()
	if err != nil {
		vlog.Fatal("securityflag.PermissionsFromFlag() failed: ", err)
	}
	if perms != nil {
		vlog.Infof("Read permissions from command line flag: %v", server.PermsString(perms))
	}
	service, err := server.NewService(ctx, nil, server.ServiceOptions{
		Perms:   perms,
		RootDir: *rootDir,
		Engine:  *engine,
	})
	if err != nil {
		vlog.Fatal("server.NewService() failed: ", err)
	}
	d := server.NewDispatcher(service)

	// Publish the service in the mount table.
	ctx, s, err := v23.WithNewDispatchingServer(ctx, *name, d)
	if err != nil {
		vlog.Fatal("v23.WithNewDispatchingServer() failed: ", err)
	}
	if eps := s.Status().Endpoints; len(eps) > 0 {
		vlog.Info("Serving as: ", eps[0].Name())
	}
	if *name != "" {
		vlog.Info("Mounted at: ", *name)
	}

	if err := service.AddNames(ctx, s); err != nil {
		vlog.Fatal("AddNames failed: ", err)
	}

	cleanup := func() {
		s.Stop()
		service.Close()
	}

	return s, d, cleanup
}
