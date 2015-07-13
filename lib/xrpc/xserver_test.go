// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrpc_test

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/ref/lib/xrpc"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

func init() {
	test.Init()
}

type service struct{}

func (s *service) Yo(ctx *context.T, call rpc.ServerCall) (string, error) {
	return "yo", nil
}

func TestXServer(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	server, err := xrpc.NewServer(ctx, "", &service{}, nil)
	if err != nil {
		t.Fatalf("Error creating server: %v", err)
	}
	ep := server.Status().Endpoints[0]

	var out string
	if err := v23.GetClient(ctx).Call(ctx, ep.Name(), "Yo", nil, []interface{}{&out}); err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if out != "yo" {
		t.Fatalf("Wanted yo, got %s", out)
	}
}

type dispatcher struct{}

func (d *dispatcher) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
	return &service{}, nil, nil
}

func TestXDispatchingServer(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	server, err := xrpc.NewDispatchingServer(ctx, "", &dispatcher{})
	if err != nil {
		t.Fatalf("Error creating server: %v", err)
	}
	ep := server.Status().Endpoints[0]

	var out string
	if err := v23.GetClient(ctx).Call(ctx, ep.Name(), "Yo", nil, []interface{}{&out}); err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if out != "yo" {
		t.Fatalf("Wanted yo, got %s", out)
	}
}
