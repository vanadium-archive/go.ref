// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/test"
)

func init() {
	test.Init()
}

func setupConns(t *testing.T, ctx *context.T, dflows, aflows chan<- flow.Flow) (dialed, accepted *Conn) {
	dmrw, amrw := newMRWPair(ctx)
	versions := version.RPCVersionRange{Min: 3, Max: 5}
	d, err := NewDialed(ctx, dmrw, nil, nil, versions, fh(dflows), nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	a, err := NewAccepted(ctx, amrw, nil, security.Blessings{}, versions, fh(aflows))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	return d, a
}

func testWrite(t *testing.T, ctx *context.T, dialer *Conn, flows <-chan flow.Flow) {
	df, err := dialer.Dial(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	want := "hello world"
	df.WriteMsgAndClose([]byte(want[:5]), []byte(want[5:]))
	af := <-flows
	msg, err := af.ReadMsg()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if got := string(msg); got != want {
		t.Errorf("Got: %s want %s", got, want)
	}
}

func TestDailerDialsFlow(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	aflows := make(chan flow.Flow, 1)
	d, _ := setupConns(t, ctx, nil, aflows)
	testWrite(t, ctx, d, aflows)
}

func TestAcceptorDialsFlow(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	dflows := make(chan flow.Flow, 1)
	_, a := setupConns(t, ctx, dflows, nil)
	testWrite(t, ctx, a, dflows)
}

// TODO(mattr): List of tests to write
// 1. multiple writes
// 2. interleave writemsg and write
// 3. interleave read and readmsg
// 4. multiple reads
