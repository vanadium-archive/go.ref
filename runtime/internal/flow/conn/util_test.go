// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	securitylib "v.io/x/ref/lib/security"
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

func setupConns(t *testing.T,
	dctx, actx *context.T,
	dflows, aflows chan<- flow.Flow) (dialed, accepted *Conn, _ *flowtest.Wire) {
	return setupConnsWithEvents(t, dctx, actx, dflows, aflows, nil)
}

func setupConnsWithEvents(t *testing.T,
	dctx, actx *context.T,
	dflows, aflows chan<- flow.Flow,
	events chan<- StatusUpdate) (dialed, accepted *Conn, _ *flowtest.Wire) {
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
		d, err := NewDialed(dctx, dmrw, ep, ep, versions, handler, events)
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
		a, err := NewAccepted(actx, amrw, ep, versions, handler, events)
		if err != nil {
			panic(err)
		}
		ach <- a
	}()
	return <-dch, <-ach, w
}

func setupFlow(t *testing.T, dctx, actx *context.T, dialFromDialer bool) (dialed flow.Flow, accepted <-chan flow.Flow, close func()) {
	dflows, aflows := make(chan flow.Flow, 1), make(chan flow.Flow, 1)
	d, a, _ := setupConns(t, dctx, actx, dflows, aflows)
	if !dialFromDialer {
		d, a = a, d
		dctx, actx = actx, dctx
		aflows, dflows = dflows, aflows
	}
	df, err := d.Dial(dctx, testBFP)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	return df, aflows, func() { d.Close(dctx, nil); a.Close(actx, nil) }
}

func testBFP(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) (security.Blessings, map[string]security.Discharge, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
}

func makeBFP(in security.Blessings) flow.BlessingsForPeer {
	return func(
		ctx *context.T,
		localEndpoint, remoteEndpoint naming.Endpoint,
		remoteBlessings security.Blessings,
		remoteDischarges map[string]security.Discharge,
	) (security.Blessings, map[string]security.Discharge, error) {
		dis := securitylib.PrepareDischarges(
			ctx, in, security.DischargeImpetus{}, time.Minute)
		return in, dis, nil
	}
}
