// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transition

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	_ "v.io/x/ref/runtime/factories/generic"
	irpc "v.io/x/ref/runtime/internal/rpc"
	"v.io/x/ref/runtime/internal/rpc/stream/manager"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

type example struct{}

func (e *example) Echo(ctx *context.T, call rpc.ServerCall, arg string) (string, error) {
	return arg, nil
}

func TestTransitionToNew(t *testing.T) {
	// TODO(mattr): Make a test showing the transition client
	// connecting to a new server once the new server is available.
}

func TestTransitionToOld(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	sm := manager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()

	sp := testutil.NewPrincipal()
	testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx)).Bless(sp, "server")
	server, err := irpc.DeprecatedNewServer(ctx, sm, v23.GetNamespace(ctx),
		nil, "", v23.GetClient(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.Listen(v23.GetListenSpec(ctx)); err != nil {
		t.Fatal(err)
	}
	if err = server.Serve("echo", &example{}, nil); err != nil {
		t.Fatal(err)
	}

	var result string
	err = v23.GetClient(ctx).Call(ctx, "echo", "Echo",
		[]interface{}{"hello"}, []interface{}{&result})
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Errorf("got %s, wanted hello", result)
	}
}
