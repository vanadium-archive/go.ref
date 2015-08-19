// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/x/ref/runtime/internal/flow/flowtest"
)

type fh chan<- flow.Flow

func (fh fh) HandleFlow(f flow.Flow) error {
	if fh == nil {
		panic("writing to nil flow handler")
	}
	fh <- f
	return nil
}

func setupConns(t *testing.T, dctx, actx *context.T, dflows, aflows chan<- flow.Flow) (dialed, accepted *Conn, _ *flowtest.Wire) {
	dmrw, amrw, w := flowtest.NewMRWPair(dctx)
	versions := version.RPCVersionRange{Min: 3, Max: 5}
	ep, err := v23.NewEndpoint("localhost:80")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	dch := make(chan *Conn)
	ach := make(chan *Conn)
	go func() {
		var handler FlowHandler
		if dflows != nil {
			handler = fh(dflows)
		}
		d, err := NewDialed(dctx, dmrw, ep, ep, versions, handler)
		if err != nil {
			panic(err)
		}
		dch <- d
	}()
	go func() {
		var handler FlowHandler
		if aflows != nil {
			handler = fh(aflows)
		}
		a, err := NewAccepted(actx, amrw, ep, versions, handler)
		if err != nil {
			panic(err)
		}
		ach <- a
	}()
	return <-dch, <-ach, w
}

func setupFlow(t *testing.T, dctx, actx *context.T, dialFromDialer bool) (dialed flow.Flow, accepted <-chan flow.Flow) {
	d, accepted := setupFlows(t, dctx, actx, dialFromDialer, 1)
	return d[0], accepted
}

func setupFlows(t *testing.T, dctx, actx *context.T, dialFromDialer bool, n int) (dialed []flow.Flow, accepted <-chan flow.Flow) {
	dflows, aflows := make(chan flow.Flow, n), make(chan flow.Flow, n)
	d, a, _ := setupConns(t, dctx, actx, dflows, aflows)
	if !dialFromDialer {
		d, a = a, d
		aflows, dflows = dflows, aflows
	}
	dialed = make([]flow.Flow, n)
	for i := 0; i < n; i++ {
		var err error
		if dialed[i], err = d.Dial(dctx, testBFP); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	}
	return dialed, aflows
}

func testBFP(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) (security.Blessings, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil
}

func makeBFP(in security.Blessings) flow.BlessingsForPeer {
	return func(
		ctx *context.T,
		localEndpoint, remoteEndpoint naming.Endpoint,
		remoteBlessings security.Blessings,
		remoteDischarges map[string]security.Discharge,
	) (security.Blessings, error) {
		return in, nil
	}
}
