// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager_test

import (
	"testing"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/manager"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

func TestListenErrors(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := manager.InternalNew(ctx, naming.FixedRoutingID(0x1))
	pserver := testutil.NewPrincipal("server")
	ctx, _ = v23.WithPrincipal(ctx, pserver)

	// principal, no blessings
	_, _, err := server.Listen(ctx, "tcp", "127.0.0.1:0", security.Blessings{}, nil)
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// blessings, no principal
	nilctx, _ := v23.WithPrincipal(ctx, nil)
	_, _, err = server.Listen(nilctx, "tcp", "127.0.0.1:0", pserver.BlessingStore().Default(), nil)
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// bad protocol
	_, _, err = server.Listen(ctx, "foo", "127.0.0.1:0", pserver.BlessingStore().Default())
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// bad address
	_, _, err = server.Listen(ctx, "tcp", "xx.0.0.1:0", pserver.BlessingStore().Default())
	if verror.ErrorID(err) != stream.ErrNetwork.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// bad address for proxy
	_, _, err = server.Listen(ctx, "v23", "127x.0.0.1", pserver.BlessingStore().Default())
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)
}

func acceptLoop(ln stream.Listener) {
	for {
		f, err := ln.Accept()
		if err != nil {
			break
		}
		f.Close()
	}

}
