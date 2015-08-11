// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"

	"v.io/syncbase/x/ref/services/syncbase/server"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/securityflag"
	_ "v.io/x/ref/runtime/factories/roaming"
)

var (
	name    = flag.String("name", "", "Name to mount at.")
	rootDir = flag.String("root-dir", "/var/lib/syncbase", "Root dir for storage engines and other data")
	engine  = flag.String("engine", "leveldb", "Storage engine to use. Currently supported: memstore and leveldb.")
)

// defaultPerms returns a permissions object that grants all permissions to the
// provided blessing patterns.
func defaultPerms(blessingPatterns []security.BlessingPattern) access.Permissions {
	perms := access.Permissions{}
	for _, tag := range access.AllTypicalTags() {
		for _, bp := range blessingPatterns {
			perms.Add(bp, string(tag))
		}
	}
	return perms
}

// TODO(sadovsky): We return rpc.Server and rpc.Dispatcher as a quick hack to
// support Mojo.
func Serve(ctx *context.T) (rpc.Server, rpc.Dispatcher) {
	s, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatal("v23.NewServer() failed: ", err)
	}
	if _, err := s.Listen(v23.GetListenSpec(ctx)); err != nil {
		vlog.Fatal("s.Listen() failed: ", err)
	}

	perms, err := securityflag.PermissionsFromFlag()
	if err != nil {
		vlog.Fatal("securityflag.PermissionsFromFlag() failed: ", err)
	}
	if perms != nil {
		vlog.Info("Using perms from command line flag.")
	} else {
		vlog.Info("Perms flag not set. Giving local principal all perms.")
		perms = defaultPerms(security.DefaultBlessingPatterns(v23.GetPrincipal(ctx)))
	}
	vlog.Infof("Perms: %v", perms)
	service, err := server.NewService(ctx, nil, server.ServiceOptions{
		Perms:   perms,
		RootDir: *rootDir,
		Engine:  *engine,
		Server:  s,
	})
	if err != nil {
		vlog.Fatal("server.NewService() failed: ", err)
	}
	d := server.NewDispatcher(service)

	// Publish the service in the mount table.
	if err := s.ServeDispatcher(*name, d); err != nil {
		vlog.Fatal("s.ServeDispatcher() failed: ", err)
	}
	if *name != "" {
		vlog.Info("Mounted at: ", *name)
	}

	return s, d
}
