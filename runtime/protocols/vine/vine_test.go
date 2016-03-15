// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vine_test

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/runtime/protocols/vine"
	"v.io/x/ref/test"
)

func TestVineOutgoingReachable(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	if err := vine.Init(ctx, "vineserver", security.AllowEveryone()); err != nil {
		t.Fatal(err)
	}
	// Create reachable and unreachable server.
	ctx, cancel := context.WithCancel(ctx)
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"vine", vine.CreateVineAddress("tcp", "127.0.0.1:0", "reachable")}}})
	_, reachServer, err := v23.WithNewServer(ctx, "reachable", &testService{}, security.AllowEveryone())
	if err != nil {
		t.Error(err)
	}
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"vine", vine.CreateVineAddress("tcp", "127.0.0.1:0", "unreachable")}}})
	_, unreachServer, err := v23.WithNewServer(ctx, "unreachable", &testService{}, security.AllowEveryone())
	if err != nil {
		t.Error(err)
	}
	defer func() {
		cancel()
		<-reachServer.Closed()
		<-unreachServer.Closed()
	}()
	// Before we set any connection behaviors, a client should be able to talk to
	// either of the servers.
	client := v23.GetClient(ctx)
	if err := client.Call(ctx, "reachable", "Foo", nil, nil); err != nil {
		t.Error(err)
	}
	if err := client.Call(ctx, "unreachable", "Foo", nil, nil); err != nil {
		t.Error(err)
	}

	// Now, we set connection behaviors that say that a client can reach "reachable"
	// but cannot reach unreachable.
	vineClient := vine.VineClient("vineserver")
	if err := vineClient.SetOutgoingBehaviors(ctx, map[string]vine.ConnBehavior{
		"reachable":   {Reachable: true},
		"unreachable": {Reachable: false},
	}); err != nil {
		t.Error(err)
	}
	// We create a new client to avoid using cached connections.
	ctx, client, err = v23.WithNewClient(ctx)
	if err != nil {
		t.Error(err)
	}
	// The call to reachable should succeed
	if err := client.Call(ctx, "reachable", "Foo", nil, nil); err != nil {
		t.Error(err)
	}
	// but the call to unreachable should fail.
	if err := client.Call(ctx, "unreachable", "Foo", nil, nil, options.NoRetry{}); err == nil {
		t.Errorf("wanted call to fail")
	}
}

type testService struct{}

func (*testService) Foo(*context.T, rpc.ServerCall) error {
	return nil
}
