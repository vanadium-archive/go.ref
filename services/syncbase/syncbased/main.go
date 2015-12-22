// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/securityflag"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/syncbase/server"
)

var (
	name                  = flag.String("name", "", "Name to mount at.")
	rootDir               = flag.String("root-dir", "/var/lib/syncbase", "Root dir for storage engines and other data.")
	engine                = flag.String("engine", "leveldb", "Storage engine to use. Currently supported: memstore and leveldb.")
	publishInNeighborhood = flag.Bool("publish-nh", true, "Whether to publish in the neighborhood.")
	devMode               = flag.Bool("dev", false, "Whether to run in development mode; required for RPCs such as Service.DevModeUpdateVClock.")
)

// Note: We return rpc.Server and rpc.Dispatcher as a quick hack to support
// Mojo.
func Serve(ctx *context.T) (rpc.Server, rpc.Dispatcher, func()) {
	perms, err := securityflag.PermissionsFromFlag()
	if err != nil {
		vlog.Fatal("securityflag.PermissionsFromFlag() failed: ", err)
	}
	if perms != nil {
		vlog.Infof("Read permissions from command line flag: %v", server.PermsString(perms))
	}
	service, err := server.NewService(ctx, server.ServiceOptions{
		Perms:                 perms,
		RootDir:               *rootDir,
		Engine:                *engine,
		PublishInNeighborhood: *publishInNeighborhood,
		DevMode:               *devMode,
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

	// Publish syncgroups and such in the various places that they should be
	// published.
	// TODO(sadovsky): Improve comments (and perhaps method name) for AddNames.
	// It's not just publishing more names in the default mount table, and under
	// certain configurations it also publishes to the neighborhood.
	if err := service.AddNames(ctx, s); err != nil {
		vlog.Fatal("AddNames failed: ", err)
	}

	// Print mount name and endpoint.
	if *name != "" {
		vlog.Info("Mounted at: ", *name)
	}
	if eps := s.Status().Endpoints; len(eps) > 0 {
		// Integration tests wait for this to be printed before trying to access the
		// service.
		fmt.Printf("ENDPOINT=%s\n", eps[0].Name())
	}

	cleanup := func() {
		s.Stop()
		service.Close()
	}

	return s, d, cleanup
}
