// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/x/ref/examples/fortune"
)

func TestGet(t *testing.T) {
	ctx, client, shutdown := setup(t)
	defer shutdown()

	value, err := client.Get(ctx)
	if err != nil {
		t.Errorf("Should not error")
	}

	if value == "" {
		t.Errorf("Expected non-empty fortune")
	}
}

func TestAdd(t *testing.T) {
	ctx, client, shutdown := setup(t)
	defer shutdown()

	value := "Lucky numbers: 12 2 35 46 56 4"
	err := client.Add(ctx, value)
	if err != nil {
		t.Errorf("Should not error")
	}

	added, err := client.Has(ctx, value)
	if err != nil {
		t.Errorf("Should not error")
	}

	if !added {
		t.Errorf("Expected service to add fortune")
	}
}

func setup(t *testing.T) (*context.T, fortune.FortuneClientStub, v23.Shutdown) {
	ctx, shutdown := v23.Init()

	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Errorf("Failure creating server")
	}

	spec := v23.GetListenSpec(ctx)
	endpoints, err := server.Listen(spec)
	if err != nil {
		t.Errorf("Error listening")
	}

	authorizer := security.DefaultAuthorizer()
	impl := newImpl()
	service := fortune.FortuneServer(impl)
	name := ""

	err = server.Serve(name, service, authorizer)
	if err != nil {
		t.Errorf("Error Serving")
	}

	endpoint := endpoints[0].Name()
	client := fortune.FortuneClient(endpoint)

	return ctx, client, shutdown
}
