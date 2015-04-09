// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server_test

// Note: core/veyron/services/security/groups/server/server_test.go has some
// helpful code snippets to model after.

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/lib/vlog"

	"v.io/syncbase/x/ref/services/syncbase/server"
	"v.io/syncbase/x/ref/services/syncbase/store/memstore"
	_ "v.io/x/ref/profiles"
	tsecurity "v.io/x/ref/test/testutil"
)

func defaultPermissions() access.Permissions {
	acl := access.Permissions{}
	for _, tag := range access.AllTypicalTags() {
		acl.Add(security.BlessingPattern("server/client"), string(tag))
	}
	return acl
}

func newServer(ctx *context.T, acl access.Permissions) (string, func()) {
	s, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatal("v23.NewServer() failed: ", err)
	}
	eps, err := s.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		vlog.Fatal("s.Listen() failed: ", err)
	}

	service := server.NewService(memstore.New())
	if acl == nil {
		acl = defaultPermissions()
	}
	if err := service.Create(acl); err != nil {
		vlog.Fatal("service.Create() failed: ", err)
	}
	d := server.NewDispatcher(service)

	if err := s.ServeDispatcher("", d); err != nil {
		vlog.Fatal("s.ServeDispatcher() failed: ", err)
	}

	name := naming.JoinAddressName(eps[0].String(), "")
	return name, func() {
		s.Stop()
	}
}

func setupOrDie(acl access.Permissions) (clientCtx *context.T, serverName string, cleanup func()) {
	ctx, shutdown := v23.Init()
	cp, sp := tsecurity.NewPrincipal("client"), tsecurity.NewPrincipal("server")

	// Have the server principal bless the client principal as "client".
	blessings, err := sp.Bless(cp.PublicKey(), sp.BlessingStore().Default(), "client", security.UnconstrainedUse())
	if err != nil {
		vlog.Fatal("sp.Bless() failed: ", err)
	}
	// Have the client present its "client" blessing when talking to the server.
	if _, err := cp.BlessingStore().Set(blessings, "server"); err != nil {
		vlog.Fatal("cp.BlessingStore().Set() failed: ", err)
	}
	// Have the client treat the server's public key as an authority on all
	// blessings that match the pattern "server".
	if err := cp.AddToRoots(blessings); err != nil {
		vlog.Fatal("cp.AddToRoots() failed: ", err)
	}

	clientCtx, err = v23.SetPrincipal(ctx, cp)
	if err != nil {
		vlog.Fatal("v23.SetPrincipal() failed: ", err)
	}
	serverCtx, err := v23.SetPrincipal(ctx, sp)
	if err != nil {
		vlog.Fatal("v23.SetPrincipal() failed: ", err)
	}

	serverName, stopServer := newServer(serverCtx, acl)
	cleanup = func() {
		stopServer()
		shutdown()
	}
	return
}

////////////////////////////////////////
// Test cases

// TODO(sadovsky): Write some tests.
func TestSomething(t *testing.T) {
	_, _, cleanup := setupOrDie(nil)
	defer cleanup()
}
