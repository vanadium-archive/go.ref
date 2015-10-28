// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package security_test

import (
	"fmt"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	securitylib "v.io/x/ref/lib/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

type expiryDischarger struct {
	called bool
}

func (ed *expiryDischarger) Discharge(ctx *context.T, call rpc.StreamServerCall, cav security.Caveat, _ security.DischargeImpetus) (security.Discharge, error) {
	tp := cav.ThirdPartyDetails()
	if tp == nil {
		return security.Discharge{}, fmt.Errorf("discharger: %v does not represent a third-party caveat", cav)
	}
	if err := tp.Dischargeable(ctx, call.Security()); err != nil {
		return security.Discharge{}, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", cav, err)
	}
	expDur := 10 * time.Millisecond
	if ed.called {
		expDur = time.Second
	}
	expiry, err := security.NewExpiryCaveat(time.Now().Add(expDur))
	if err != nil {
		return security.Discharge{}, fmt.Errorf("failed to create an expiration on the discharge: %v", err)
	}
	d, err := call.Security().LocalPrincipal().MintDischarge(cav, expiry)
	if err != nil {
		return security.Discharge{}, err
	}
	ctx.Infof("got discharge on sever %#v", d)
	ed.called = true
	return d, nil
}

func TestPrepareDischarges(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	pclient := testutil.NewPrincipal("client")
	cctx, err := v23.WithPrincipal(ctx, pclient)
	if err != nil {
		t.Fatal(err)
	}
	pdischarger := testutil.NewPrincipal("discharger")
	dctx, err := v23.WithPrincipal(ctx, pdischarger)
	if err != nil {
		t.Fatal(err)
	}
	security.AddToRoots(pclient, pdischarger.BlessingStore().Default())
	security.AddToRoots(pclient, v23.GetPrincipal(ctx).BlessingStore().Default())
	security.AddToRoots(pdischarger, pclient.BlessingStore().Default())
	security.AddToRoots(pdischarger, v23.GetPrincipal(ctx).BlessingStore().Default())

	expcav, err := security.NewExpiryCaveat(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	tpcav, err := security.NewPublicKeyCaveat(
		pdischarger.PublicKey(),
		"discharger",
		security.ThirdPartyRequirements{},
		expcav)
	if err != nil {
		t.Fatal(err)
	}
	cbless, err := pclient.BlessSelf("clientcaveats", tpcav)
	if err != nil {
		t.Fatal(err)
	}
	tpid := tpcav.ThirdPartyDetails().ID()

	v23.GetPrincipal(dctx)
	dctx, _, err = v23.WithNewServer(dctx,
		"discharger",
		&expiryDischarger{},
		security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}

	// Fetch discharges for tpcav.
	buffer := 100 * time.Millisecond
	discharges := securitylib.PrepareDischarges(cctx, cbless,
		security.DischargeImpetus{}, buffer)
	if len(discharges) != 1 {
		t.Errorf("Got %d discharges, expected 1.", len(discharges))
	}
	dis, has := discharges[tpid]
	if !has {
		t.Errorf("Got %#v, Expected discharge for %s", discharges, tpid)
	}
	// Check that the discharges is not yet expired, but is expired after 100 milliseconds.
	expiry := dis.Expiry()
	// The discharge should expire.
	select {
	case <-time.After(time.Now().Sub(expiry)):
		break
	case <-time.After(time.Second):
		t.Fatalf("discharge didn't expire within a second")
	}

	// Preparing Discharges again to get fresh discharges.
	discharges = securitylib.PrepareDischarges(cctx, cbless,
		security.DischargeImpetus{}, buffer)
	if len(discharges) != 1 {
		t.Errorf("Got %d discharges, expected 1.", len(discharges))
	}
	dis, has = discharges[tpid]
	if !has {
		t.Errorf("Got %#v, Expected discharge for %s", discharges, tpid)
	}
	now := time.Now()
	if expiry = dis.Expiry(); expiry.Before(now) {
		t.Fatalf("discharge has expired %v, but should be fresh", dis)
	}
}
