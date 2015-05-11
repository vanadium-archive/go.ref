// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/services/binary"
	"v.io/v23/services/build"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

//go:generate v23 test generate

type mock struct{}

func (mock) Build(ctx *context.T, call build.BuilderBuildServerCall, arch build.Architecture, opsys build.OperatingSystem) ([]byte, error) {
	vlog.VI(2).Infof("Build(%v, %v) was called", arch, opsys)
	iterator := call.RecvStream()
	for iterator.Advance() {
	}
	if err := iterator.Err(); err != nil {
		vlog.Errorf("Advance() failed: %v", err)
		return nil, verror.New(verror.ErrInternal, ctx)
	}
	return nil, nil
}

func (mock) Describe(_ *context.T, _ rpc.ServerCall, name string) (binary.Description, error) {
	vlog.VI(2).Infof("Describe(%v) was called", name)
	return binary.Description{}, nil
}

type dispatcher struct{}

func startServer(ctx *context.T, t *testing.T) (rpc.Server, naming.Endpoint) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	l := v23.GetListenSpec(ctx)
	endpoints, err := server.Listen(l)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", l, err)
	}
	unpublished := ""
	if err := server.Serve(unpublished, build.BuilderServer(&mock{}), nil); err != nil {
		t.Fatalf("Serve(%v) failed: %v", unpublished, err)
	}
	return server, endpoints[0]
}

func stopServer(t *testing.T, server rpc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("Stop() failed: %v", err)
	}
}

func TestBuildClient(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	server, endpoint := startServer(ctx, t)
	defer stopServer(t, server)

	var stdout, stderr bytes.Buffer
	env := &cmdline.Env{Stdout: &stdout, Stderr: &stderr}
	args := []string{"build", naming.JoinAddressName(endpoint.String(), ""), "v.io/x/ref/services/build/build"}
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, args); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got, want := strings.TrimSpace(stdout.String()), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
