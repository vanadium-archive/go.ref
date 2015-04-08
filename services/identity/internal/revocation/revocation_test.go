// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package revocation

import (
	"testing"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/services/discharger"
	"v.io/x/ref/services/identity/internal/dischargerlib"
	"v.io/x/ref/test"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
)

//go:generate v23 test generate

func revokerSetup(t *testing.T, ctx *context.T) (dischargerKey security.PublicKey, dischargerEndpoint string, revoker RevocationManager, closeFunc func()) {
	revokerService := NewMockRevocationManager()
	dischargerServer, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("r.NewServer: %s", err)
	}
	dischargerEPs, err := dischargerServer.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		t.Fatalf("dischargerServer.Listen failed: %v", err)
	}
	dischargerServiceStub := discharger.DischargerServer(dischargerlib.NewDischarger())
	if err := dischargerServer.Serve("", dischargerServiceStub, nil); err != nil {
		t.Fatalf("dischargerServer.Serve revoker: %s", err)
	}
	return v23.GetPrincipal(ctx).PublicKey(),
		dischargerEPs[0].Name(),
		revokerService,
		func() {
			dischargerServer.Stop()
		}
}

func TestDischargeRevokeDischargeRevokeDischarge(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	dcKey, dc, revoker, closeFunc := revokerSetup(t, ctx)
	defer closeFunc()

	discharger := discharger.DischargerClient(dc)
	caveat, err := revoker.NewCaveat(dcKey, dc)
	if err != nil {
		t.Fatalf("failed to create revocation caveat: %s", err)
	}
	tp := caveat.ThirdPartyDetails()
	if tp == nil {
		t.Fatalf("failed to extract third party details from caveat %v", caveat)
	}

	var impetus security.DischargeImpetus
	if _, err := discharger.Discharge(ctx, caveat, impetus); err != nil {
		t.Fatalf("failed to get discharge: %s", err)
	}
	if err := revoker.Revoke(tp.ID()); err != nil {
		t.Fatalf("failed to revoke: %s", err)
	}
	if _, err := discharger.Discharge(ctx, caveat, impetus); err == nil {
		t.Fatalf("got a discharge for a revoked caveat: %s", err)
	}
	if err := revoker.Revoke(tp.ID()); err != nil {
		t.Fatalf("failed to revoke again: %s", err)
	}
	if _, err := discharger.Discharge(ctx, caveat, impetus); err == nil {
		t.Fatalf("got a discharge for a doubly revoked caveat: %s", err)
	}
}
