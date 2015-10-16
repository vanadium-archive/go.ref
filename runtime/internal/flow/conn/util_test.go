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
	dflows, aflows chan<- flow.Flow,
	noencrypt bool) (dialed, accepted *Conn, _ *flowtest.Wire) {
	var dmrw, amrw *flowtest.MRW
	var w *flowtest.Wire
	if noencrypt {
		dmrw, amrw, w = flowtest.NewUnencryptedMRWPair(dctx)
	} else {
		dmrw, amrw, w = flowtest.NewMRWPair(dctx)
	}
	versions := version.RPCVersionRange{Min: 3, Max: 5}
	ridep, err := v23.NewEndpoint("@6@@batman.com:1234@@000000000000000000000000dabbad00@m@@@")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	ep, err := v23.NewEndpoint("localhost:80")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	dch := make(chan *Conn)
	ach := make(chan *Conn)
	go func() {
		var handler FlowHandler
		dep := ep
		if dflows != nil {
			handler = fh(dflows)
			dep = ridep
		}
		dBlessings := v23.GetPrincipal(dctx).BlessingStore().Default()
		d, err := NewDialed(dctx, dBlessings, dmrw, dep, ep, versions, flowtest.AllowAllPeersAuthorizer{}, time.Minute, handler)
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
		aBlessings := v23.GetPrincipal(actx).BlessingStore().Default()
		a, err := NewAccepted(actx, aBlessings, amrw, ridep, versions, time.Minute, handler)
		if err != nil {
			panic(err)
		}
		ach <- a
	}()
	return <-dch, <-ach, w
}

func setupFlow(t *testing.T, dctx, actx *context.T, dialFromDialer bool) (dialed flow.Flow, accepted <-chan flow.Flow, close func()) {
	dfs, accepted, ac, dc := setupFlows(t, dctx, actx, dialFromDialer, 1, false)
	return dfs[0], accepted, func() { dc.Close(dctx, nil); ac.Close(dctx, nil) }
}

func setupFlows(t *testing.T, dctx, actx *context.T, dialFromDialer bool, n int, noencrypt bool) (dialed []flow.Flow, accepted <-chan flow.Flow, dc, ac *Conn) {
	dialed = make([]flow.Flow, n)
	dflows, aflows := make(chan flow.Flow, n), make(chan flow.Flow, n)
	d, a, _ := setupConns(t, dctx, actx, dflows, aflows, noencrypt)
	if !dialFromDialer {
		d, a = a, d
		dctx, actx = actx, dctx
		aflows, dflows = dflows, aflows
	}
	for i := 0; i < n; i++ {
		var err error
		if dialed[i], err = d.Dial(dctx, flowtest.AllowAllPeersAuthorizer{}, nil); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	}
	return dialed, aflows, d, a
}

type peerAuthorizer struct {
	blessings security.Blessings
}

func (peerAuthorizer) AuthorizePeer(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) ([]string, []security.RejectedBlessing, error) {
	return nil, nil, nil
}

func (a peerAuthorizer) BlessingsForPeer(ctx *context.T, _ []string) (
	security.Blessings, map[string]security.Discharge, error) {
	dis := securitylib.PrepareDischarges(ctx, a.blessings, security.DischargeImpetus{}, time.Minute)
	return a.blessings, dis, nil
}
