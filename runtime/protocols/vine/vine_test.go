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

func TestOutgoingReachable(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	ctx, err := vine.Init(ctx, "vineserver", security.AllowEveryone(), "client")
	if err != nil {
		t.Fatal(err)
	}
	// Create reachable and unreachable server, ensure they have corresponding tags set.
	ctx, cancel := context.WithCancel(ctx)
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"vine", "tcp/127.0.0.1:0"}}})
	rctx := vine.WithLocalTag(ctx, "reachable")
	uctx := vine.WithLocalTag(ctx, "unreachable")
	_, reachServer, err := v23.WithNewServer(rctx, "reachable", &testService{}, security.AllowEveryone())
	if err != nil {
		t.Error(err)
	}
	_, unreachServer, err := v23.WithNewServer(uctx, "unreachable", &testService{}, security.AllowEveryone())
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
	// We create a new client to avoid using cached connections.
	ctx, client, err = v23.WithNewClient(ctx)
	if err != nil {
		t.Error(err)
	}
	if err := client.Call(ctx, "unreachable", "Foo", nil, nil); err != nil {
		t.Error(err)
	}

	// Now, we set connection behaviors that say that "client" can reach "reachable"
	// but cannot reach unreachable.
	vineClient := vine.VineClient("vineserver")
	if err := vineClient.SetBehaviors(ctx, map[vine.ConnKey]vine.ConnBehavior{
		vine.ConnKey{"client", "reachable"}:   {Reachable: true},
		vine.ConnKey{"client", "unreachable"}: {Reachable: false},
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
	// We create a new client to avoid using cached connections.
	ctx, client, err = v23.WithNewClient(ctx)
	if err != nil {
		t.Error(err)
	}
	// but the call to unreachable should fail.
	if err := client.Call(ctx, "unreachable", "Foo", nil, nil, options.NoRetry{}); err == nil {
		t.Errorf("wanted call to fail")
	}
}

func TestIncomingReachable(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	ctx, err := vine.Init(ctx, "vineserver", security.AllowEveryone(), "client")
	if err != nil {
		t.Fatal(err)
	}
	denyCtx := vine.WithLocalTag(ctx, "denyClient")

	sctx := vine.WithLocalTag(ctx, "server")
	sctx, cancel := context.WithCancel(sctx)
	sctx = v23.WithListenSpec(sctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"vine", "tcp/127.0.0.1:0"}}})
	_, server, err := v23.WithNewServer(sctx, "server", &testService{}, security.AllowEveryone())
	if err != nil {
		t.Error(err)
	}
	defer func() {
		cancel()
		<-server.Closed()
	}()

	// Before setting a policy all calls should succeed.
	if err := v23.GetClient(ctx).Call(ctx, "server", "Foo", nil, nil); err != nil {
		t.Error(err)
	}
	if err := v23.GetClient(denyCtx).Call(ctx, "server", "Foo", nil, nil); err != nil {
		t.Error(err)
	}

	// Create new clients to avoid using cached connections.
	if ctx, _, err = v23.WithNewClient(ctx); err != nil {
		t.Error(err)
	}
	if denyCtx, _, err = v23.WithNewClient(denyCtx); err != nil {
		t.Error(err)
	}

	// Set a policy that allows "server" to accept connections from "client" but
	// denies all connections from "denyClient".
	vineClient := vine.VineClient("vineserver")
	if err := vineClient.SetBehaviors(ctx, map[vine.ConnKey]vine.ConnBehavior{
		vine.ConnKey{"client", "server"}:     {Reachable: true},
		vine.ConnKey{"denyClient", "server"}: {Reachable: false},
	}); err != nil {
		t.Error(err)
	}

	// Now, the call from client to server should work.
	if err := v23.GetClient(ctx).Call(ctx, "server", "Foo", nil, nil); err != nil {
		t.Error(err)
	}
	// but the call from denyclient to server should fail.
	if err := v23.GetClient(denyCtx).Call(denyCtx, "server", "Foo", nil, nil, options.NoRetry{}); err == nil {
		t.Errorf("wanted call to fail")
	}
}

type testService struct{}

func (*testService) Foo(*context.T, rpc.ServerCall) error {
	return nil
}
