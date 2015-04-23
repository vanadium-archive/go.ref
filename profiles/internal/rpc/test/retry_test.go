// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
)

var errRetryThis = verror.Register("retry_test.retryThis", verror.RetryBackoff, "retryable error")

type retryServer struct {
	called int // number of times TryAgain has been called
}

func (s *retryServer) TryAgain(ctx *context.T, _ rpc.ServerCall) error {
	// If this is the second time this method is being called, return success.
	if s.called > 0 {
		s.called++
		return nil
	}
	s.called++
	// otherwise, return a verror with action code RetryBackoff.
	return verror.New(errRetryThis, ctx)
}

func TestRetryCall(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	// Start the server.
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eps, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		t.Fatal(err)
	}
	rs := retryServer{}
	if err = server.Serve("", &rs, security.AllowEveryone()); err != nil {
		t.Fatal(err)
	}
	name := eps[0].Name()

	client := v23.GetClient(ctx)
	// A traditional client.StartCall/call.Finish sequence should fail at
	// call.Finish since the error returned by the server won't be retried.
	call, err := client.StartCall(ctx, name, "TryAgain", nil)
	if err != nil {
		t.Errorf("client.StartCall failed: %v", err)
	}
	if err := call.Finish(); err == nil {
		t.Errorf("call.Finish should have failed")
	}
	rs.called = 0

	// A call to client.Call should succeed because the error return by the
	// server should be retried.
	if err := client.Call(ctx, name, "TryAgain", nil, nil); err != nil {
		t.Errorf("client.Call failed: %v", err)
	}
	// Ensure that client.Call retried the call exactly once.
	if rs.called != 2 {
		t.Errorf("retryServer should have been called twice, instead called %d times", rs.called)
	}
	rs.called = 0

	// The options.NoRetry option should be honored by client.Call, so the following
	// call should fail.
	if err := client.Call(ctx, name, "TryAgain", nil, nil, options.NoRetry{}); err == nil {
		t.Errorf("client.Call(..., options.NoRetry{}) should have failed")
	}
	// Ensure that client.Call did not retry the call.
	if rs.called != 1 {
		t.Errorf("retryServer have been called once, instead called %d times", rs.called)
	}
}
