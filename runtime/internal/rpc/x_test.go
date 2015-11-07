// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/runtime/internal/flow/manager"
)

type testService struct{}

func (t *testService) Echo(ctx *context.T, call rpc.ServerCall, arg string) (string, error) {
	return "response:" + arg, nil
}

func TestXClientServer(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	var i uint64 = 1
	ctx = fake.SetFlowManagerFactory(ctx, func(ctx *context.T, channelTimeout time.Duration) flow.Manager {
		i++
		return manager.New(ctx, naming.FixedRoutingID(i), nil, channelTimeout)
	})
	ctx = fake.SetClientFactory(ctx, func(ctx *context.T, o ...rpc.ClientOpt) rpc.Client {
		return NewXClient(ctx, v23.GetNamespace(ctx), o...)
	})

	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{{Protocol: "tcp", Address: "127.0.0.1:0"}},
	})
	_, _, err := WithNewServer(ctx, "server", &testService{}, nil, nil)
	if err != nil {
		t.Fatal(verror.DebugString(err))
	}
	var result string
	if err = v23.GetClient(ctx).Call(ctx, "server", "Echo", []interface{}{"hello"}, []interface{}{&result}); err != nil {
		t.Fatal(verror.DebugString(err))
	}
	if want := "response:hello"; result != want {
		t.Errorf("got %q wanted %q", result, want)
	}
}

type testDispatcher struct{}

func (t *testDispatcher) Lookup(ctx *context.T, suffix string) (interface{}, security.Authorizer, error) {
	return &testService{}, nil, nil
}

func TestXClientDispatchingServer(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	var i uint64 = 1
	ctx = fake.SetFlowManagerFactory(ctx, func(ctx *context.T, channelTimeout time.Duration) flow.Manager {
		i++
		return manager.New(ctx, naming.FixedRoutingID(i), nil, channelTimeout)
	})
	ctx = fake.SetClientFactory(ctx, func(ctx *context.T, o ...rpc.ClientOpt) rpc.Client {
		return NewXClient(ctx, v23.GetNamespace(ctx), o...)
	})

	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{{Protocol: "tcp", Address: "127.0.0.1:0"}},
	})
	_, _, err := WithNewDispatchingServer(ctx, "server", &testDispatcher{}, nil)
	if err != nil {
		t.Fatal(verror.DebugString(err))
	}
	var result string
	if err = v23.GetClient(ctx).Call(ctx, "server", "Echo", []interface{}{"hello"}, []interface{}{&result}); err != nil {
		t.Fatal(verror.DebugString(err))
	}
	if want := "response:hello"; result != want {
		t.Errorf("got %q wanted %q", result, want)
	}
}
