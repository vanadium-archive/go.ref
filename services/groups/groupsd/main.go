// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Daemon groupsd implements the v.io/v23/services/groups interfaces for
// managing access control groups.
package main

// Example invocation:
// groupsd --v23.tcp.address="127.0.0.1:0" --name=groupsd

import (
	"flag"

	"v.io/v23"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/xrpc"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/groups/internal/server"
	"v.io/x/ref/services/groups/internal/store/memstore"
)

var (
	name = flag.String("name", "", "Name to mount at.")
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

// TODO(ashankar, jsimsa): Implement groupsd using the cmdline package.
func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	perms, err := securityflag.PermissionsFromFlag()
	if err != nil {
		vlog.Fatal("securityflag.PermissionsFromFlag() failed: ", err)
	}

	if perms != nil {
		vlog.Info("Using permissions from command line flag.")
	} else {
		vlog.Info("No permissions flag provided. Giving local principal all permissions.")
		perms = defaultPerms(security.DefaultBlessingPatterns(v23.GetPrincipal(ctx)))
	}

	m := server.NewManager(memstore.New(), perms)
	if _, err = xrpc.NewDispatchingServer(ctx, *name, m); err != nil {
		vlog.Fatal("NewDispatchingServer() failed: ", err)
	}
	vlog.Info("Mounted at: ", *name)

	// Wait forever.
	<-signals.ShutdownOnSignals(ctx)
}
