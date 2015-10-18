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
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/runtime/internal/flow/flowtest"
	"v.io/x/ref/test/goroutines"
	"v.io/x/ref/test/testutil"
)

func checkBlessings(t *testing.T, got, want security.Blessings, gotd map[string]security.Discharge) {
	if !got.Equivalent(want) {
		t.Errorf("got: %v wanted %v", got, want)
	}
	tpid := got.ThirdPartyCaveats()[0].ThirdPartyDetails().ID()
	if _, has := gotd[tpid]; !has {
		t.Errorf("got: %#v wanted %s", gotd, tpid)
	}
}

func checkFlowBlessings(t *testing.T, df, af flow.Flow, db, ab security.Blessings) {
	if msg, err := af.ReadMsg(); err != nil || string(msg) != "hello" {
		t.Errorf("Got %s, %v wanted hello, nil", string(msg), err)
	}
	checkBlessings(t, af.LocalBlessings(), ab, af.LocalDischarges())
	checkBlessings(t, af.RemoteBlessings(), db, af.RemoteDischarges())
	checkBlessings(t, df.LocalBlessings(), db, df.LocalDischarges())
	checkBlessings(t, df.RemoteBlessings(), ab, df.RemoteDischarges())
}

func dialFlow(t *testing.T, ctx *context.T, dc *Conn, b security.Blessings) flow.Flow {
	df, err := dc.Dial(ctx, peerAuthorizer{b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = df.WriteMsg([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	return df
}

func BlessWithTPCaveat(t *testing.T, ctx *context.T, p security.Principal, s string) security.Blessings {
	dp := v23.GetPrincipal(ctx)
	expcav, err := security.NewExpiryCaveat(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	tpcav, err := security.NewPublicKeyCaveat(dp.PublicKey(), "discharge",
		security.ThirdPartyRequirements{}, expcav)
	if err != nil {
		t.Fatal(err)
	}
	b, err := testutil.IDProviderFromPrincipal(dp).NewBlessings(p, s, tpcav)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func NewPrincipalWithTPCaveat(t *testing.T, ctx *context.T, s string) *context.T {
	p := testutil.NewPrincipal()
	vsecurity.SetDefaultBlessings(p, BlessWithTPCaveat(t, ctx, p, s))
	ctx, err := v23.WithPrincipal(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

type fakeDischargeClient struct {
	p security.Principal
}

func (fc *fakeDischargeClient) Call(_ *context.T, _, _ string, inArgs, outArgs []interface{}, _ ...rpc.CallOpt) error {
	expiry, err := security.NewExpiryCaveat(time.Now().Add(time.Minute))
	if err != nil {
		panic(err)
	}
	*(outArgs[0].(*security.Discharge)), err = fc.p.MintDischarge(
		inArgs[0].(security.Caveat), expiry)
	return err
}
func (fc *fakeDischargeClient) StartCall(*context.T, string, string, []interface{}, ...rpc.CallOpt) (rpc.ClientCall, error) {
	return nil, nil
}
func (fc *fakeDischargeClient) Close()                  {}
func (fc *fakeDischargeClient) Closed() <-chan struct{} { return nil }

func TestUnidirectional(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	ctx = fake.SetClientFactory(ctx, func(ctx *context.T, opts ...rpc.ClientOpt) rpc.Client {
		return &fakeDischargeClient{v23.GetPrincipal(ctx)}
	})
	ctx, _, _ = v23.WithNewClient(ctx)

	dctx := NewPrincipalWithTPCaveat(t, ctx, "dialer")
	actx := NewPrincipalWithTPCaveat(t, ctx, "acceptor")
	aflows := make(chan flow.Flow, 2)
	dc, ac := setupConns(t, "local", "", dctx, actx, nil, aflows)
	defer dc.Close(dctx, nil)
	defer ac.Close(actx, nil)

	df1 := dialFlow(t, dctx, dc, v23.GetPrincipal(dctx).BlessingStore().Default())
	af1 := <-aflows
	checkFlowBlessings(t, df1, af1,
		v23.GetPrincipal(dctx).BlessingStore().Default(),
		v23.GetPrincipal(actx).BlessingStore().Default())

	db2 := BlessWithTPCaveat(t, ctx, v23.GetPrincipal(dctx), "other")
	df2 := dialFlow(t, dctx, dc, db2)
	af2 := <-aflows
	checkFlowBlessings(t, df2, af2, db2,
		v23.GetPrincipal(actx).BlessingStore().Default())

	// We should not be able to dial in the other direction, because that flow
	// manager is not willing to accept flows.
	_, err := ac.Dial(actx, flowtest.AllowAllPeersAuthorizer{}, nil)
	if verror.ErrorID(err) != ErrDialingNonServer.ID {
		t.Errorf("got %v, wanted ErrDialingNonServer", err)
	}
}

func TestBidirectional(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	ctx = fake.SetClientFactory(ctx, func(ctx *context.T, opts ...rpc.ClientOpt) rpc.Client {
		return &fakeDischargeClient{v23.GetPrincipal(ctx)}
	})
	ctx, _, _ = v23.WithNewClient(ctx)

	dctx := NewPrincipalWithTPCaveat(t, ctx, "dialer")
	actx := NewPrincipalWithTPCaveat(t, ctx, "acceptor")
	dflows := make(chan flow.Flow, 2)
	aflows := make(chan flow.Flow, 2)
	dc, ac := setupConns(t, "local", "", dctx, actx, dflows, aflows)
	defer dc.Close(dctx, nil)
	defer ac.Close(actx, nil)

	df1 := dialFlow(t, dctx, dc, v23.GetPrincipal(dctx).BlessingStore().Default())
	af1 := <-aflows
	checkFlowBlessings(t, df1, af1,
		v23.GetPrincipal(dctx).BlessingStore().Default(),
		v23.GetPrincipal(actx).BlessingStore().Default())

	db2 := BlessWithTPCaveat(t, ctx, v23.GetPrincipal(dctx), "other")
	df2 := dialFlow(t, dctx, dc, db2)
	af2 := <-aflows
	checkFlowBlessings(t, df2, af2, db2,
		v23.GetPrincipal(actx).BlessingStore().Default())

	af3 := dialFlow(t, actx, ac, v23.GetPrincipal(actx).BlessingStore().Default())
	df3 := <-dflows
	checkFlowBlessings(t, af3, df3,
		v23.GetPrincipal(actx).BlessingStore().Default(),
		v23.GetPrincipal(dctx).BlessingStore().Default())

	ab2 := BlessWithTPCaveat(t, ctx, v23.GetPrincipal(actx), "aother")
	af4 := dialFlow(t, actx, ac, ab2)
	df4 := <-dflows
	checkFlowBlessings(t, af4, df4, ab2,
		v23.GetPrincipal(dctx).BlessingStore().Default())
}
