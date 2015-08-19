// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/security"
	"v.io/v23/verror"
	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/test/goroutines"
	"v.io/x/ref/test/testutil"
)

func checkBlessings(t *testing.T, df, af flow.Flow, db, ab security.Blessings) {
	msg, err := af.ReadMsg()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "hello" {
		t.Fatalf("Got %s, wanted hello", string(msg))
	}
	if !af.LocalBlessings().Equivalent(ab) {
		t.Errorf("got: %#v wanted %#v", af.LocalBlessings(), ab)
	}
	if !af.RemoteBlessings().Equivalent(db) {
		t.Errorf("got: %#v wanted %#v", af.RemoteBlessings(), db)
	}
	if !df.LocalBlessings().Equivalent(db) {
		t.Errorf("got: %#v wanted %#v", df.LocalBlessings(), db)
	}
	if !df.RemoteBlessings().Equivalent(ab) {
		t.Errorf("got: %#v wanted %#v", df.RemoteBlessings(), ab)
	}
}
func dialFlow(t *testing.T, ctx *context.T, dc *Conn, b security.Blessings) flow.Flow {
	df, err := dc.Dial(ctx, makeBFP(b))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = df.WriteMsg([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	return df
}

func TestUnidirectional(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	dctx, shutdown := v23.Init()
	defer shutdown()
	actx, err := v23.WithPrincipal(dctx, testutil.NewPrincipal("acceptor"))
	if err != nil {
		t.Fatal(err)
	}
	aflows := make(chan flow.Flow, 2)
	dc, ac, _ := setupConns(t, dctx, actx, nil, aflows)
	defer dc.Close(dctx, nil)
	defer ac.Close(actx, nil)

	df1 := dialFlow(t, dctx, dc, v23.GetPrincipal(dctx).BlessingStore().Default())
	af1 := <-aflows
	checkBlessings(t, df1, af1,
		v23.GetPrincipal(dctx).BlessingStore().Default(),
		v23.GetPrincipal(actx).BlessingStore().Default())

	db2, err := v23.GetPrincipal(dctx).BlessSelf("other")
	if err != nil {
		t.Fatal(err)
	}
	df2 := dialFlow(t, dctx, dc, db2)
	af2 := <-aflows
	checkBlessings(t, df2, af2, db2,
		v23.GetPrincipal(actx).BlessingStore().Default())

	// We should not be able to dial in the other direction, because that flow
	// manager is not willing to accept flows.
	_, err = ac.Dial(actx, testBFP)
	if verror.ErrorID(err) != ErrDialingNonServer.ID {
		t.Errorf("got %v, wanted ErrDialingNonServer", err)
	}
}

func TestBidirectional(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	dctx, shutdown := v23.Init()
	defer shutdown()
	actx, err := v23.WithPrincipal(dctx, testutil.NewPrincipal("acceptor"))
	if err != nil {
		t.Fatal(err)
	}
	dflows := make(chan flow.Flow, 2)
	aflows := make(chan flow.Flow, 2)
	dc, ac, _ := setupConns(t, dctx, actx, dflows, aflows)
	defer dc.Close(dctx, nil)
	defer ac.Close(actx, nil)

	df1 := dialFlow(t, dctx, dc, v23.GetPrincipal(dctx).BlessingStore().Default())
	af1 := <-aflows
	checkBlessings(t, df1, af1,
		v23.GetPrincipal(dctx).BlessingStore().Default(),
		v23.GetPrincipal(actx).BlessingStore().Default())

	db2, err := v23.GetPrincipal(dctx).BlessSelf("other")
	if err != nil {
		t.Fatal(err)
	}
	df2 := dialFlow(t, dctx, dc, db2)
	af2 := <-aflows
	checkBlessings(t, df2, af2, db2,
		v23.GetPrincipal(actx).BlessingStore().Default())

	af3 := dialFlow(t, actx, ac, v23.GetPrincipal(actx).BlessingStore().Default())
	df3 := <-dflows
	checkBlessings(t, af3, df3,
		v23.GetPrincipal(actx).BlessingStore().Default(),
		v23.GetPrincipal(dctx).BlessingStore().Default())

	ab2, err := v23.GetPrincipal(actx).BlessSelf("aother")
	if err != nil {
		t.Fatal(err)
	}
	af4 := dialFlow(t, actx, ac, ab2)
	df4 := <-dflows
	checkBlessings(t, af4, df4, ab2,
		v23.GetPrincipal(dctx).BlessingStore().Default())
}
