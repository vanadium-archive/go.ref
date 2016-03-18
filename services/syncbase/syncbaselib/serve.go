// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syncbaselib

import (
	"fmt"
	"os"
	"runtime/pprof"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/services/syncbase/server"
)

// Serve starts the Syncbase server. Returns rpc.Server and rpc.Dispatcher for
// use in the Mojo bindings, along with a cleanup function.
func Serve(ctx *context.T, opts Opts) (rpc.Server, rpc.Dispatcher, func()) {
	// Note: Adding the "runtime/pprof" import does not significantly increase the
	// binary size (only ~4500 bytes), so it seems okay to expose the option to
	// profile.
	if opts.CpuProfile != "" {
		f, err := os.Create(opts.CpuProfile)
		if err != nil {
			vlog.Fatal("Unable to create the cpu profile file: ", err)
		}
		defer f.Close()
		if err = pprof.StartCPUProfile(f); err != nil {
			vlog.Fatal("StartCPUProfile failed: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	perms, err := securityflag.PermissionsFromFlag()
	if err != nil {
		vlog.Fatal("securityflag.PermissionsFromFlag() failed: ", err)
	}
	if perms != nil {
		vlog.Infof("Read permissions from command line flag: %v", server.PermsString(perms))
	}
	service, err := server.NewService(ctx, server.ServiceOptions{
		Perms:           perms,
		RootDir:         opts.RootDir,
		Engine:          opts.Engine,
		SkipPublishInNh: opts.SkipPublishInNh,
		DevMode:         opts.DevMode,
	})
	if err != nil {
		vlog.Fatal("server.NewService() failed: ", err)
	}
	d := server.NewDispatcher(service)

	// Publish the service in the mount table.
	ctx, cancel := context.WithCancel(ctx)
	ctx, s, err := v23.WithNewDispatchingServer(ctx, opts.Name, d)
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
	if opts.Name != "" {
		vlog.Info("Mounted at: ", opts.Name)
	}
	if eps := s.Status().Endpoints; len(eps) > 0 {
		// Integration tests wait for this to be printed before trying to access the
		// service.
		fmt.Printf("ENDPOINT=%s\n", eps[0].Name())
	}

	cleanup := func() {
		cancel()
		<-s.Closed()
		service.Close()
	}

	return s, d, cleanup
}
