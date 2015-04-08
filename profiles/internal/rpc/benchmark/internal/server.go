// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/profiles/internal/rpc/benchmark"
)

type impl struct {
}

func (i *impl) Echo(call rpc.ServerCall, payload []byte) ([]byte, error) {
	return payload, nil
}

func (i *impl) EchoStream(call benchmark.BenchmarkEchoStreamServerCall) error {
	rStream := call.RecvStream()
	sStream := call.SendStream()
	for rStream.Advance() {
		sStream.Send(rStream.Value())
	}
	if err := rStream.Err(); err != nil {
		return err
	}
	return nil
}

// StartServer starts a server that implements the Benchmark service. The
// server listens to the given protocol and address, and returns the vanadium
// address of the server and a callback function to stop the server.
func StartServer(ctx *context.T, listenSpec rpc.ListenSpec) (string, func()) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	eps, err := server.Listen(listenSpec)
	if err != nil {
		vlog.Fatalf("Listen failed: %v", err)
	}

	if err := server.Serve("", benchmark.BenchmarkServer(&impl{}), securityflag.NewAuthorizerOrDie()); err != nil {
		vlog.Fatalf("Serve failed: %v", err)
	}
	return eps[0].Name(), func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}
