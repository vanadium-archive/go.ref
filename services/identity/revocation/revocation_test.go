package revocation

import (
	"testing"

	"v.io/core/veyron/profiles"
	services "v.io/core/veyron/services/security"
	"v.io/core/veyron/services/security/discharger"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vom2"
)

func revokerSetup(t *testing.T, ctx *context.T) (dischargerKey security.PublicKey, dischargerEndpoint string, revoker RevocationManager, closeFunc func()) {
	revokerService := NewMockRevocationManager()
	dischargerServer, err := veyron2.NewServer(ctx)
	if err != nil {
		t.Fatalf("r.NewServer: %s", err)
	}
	dischargerEPs, err := dischargerServer.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("dischargerServer.Listen failed: %v", err)
	}
	dischargerServiceStub := services.DischargerServer(discharger.NewDischarger())
	if err := dischargerServer.Serve("", dischargerServiceStub, nil); err != nil {
		t.Fatalf("dischargerServer.Serve revoker: %s", err)
	}
	return veyron2.GetPrincipal(ctx).PublicKey(),
		naming.JoinAddressName(dischargerEPs[0].String(), ""),
		revokerService,
		func() {
			dischargerServer.Stop()
		}
}

func TestDischargeRevokeDischargeRevokeDischarge(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %v", err)
	}
	defer r.Cleanup()
	ctx := r.NewContext()

	dcKey, dc, revoker, closeFunc := revokerSetup(t, ctx)
	defer closeFunc()

	discharger := services.DischargerClient(dc)
	caveat, err := revoker.NewCaveat(dcKey, dc)
	if err != nil {
		t.Fatalf("failed to create revocation caveat: %s", err)
	}
	var cav security.ThirdPartyCaveat
	if err := vom2.Decode(caveat.ValidatorVOM, &cav); err != nil {
		t.Fatalf("failed to create decode tp caveat: %s", err)
	}

	var impetus security.DischargeImpetus

	if _, err = discharger.Discharge(r.NewContext(), cav, impetus); err != nil {
		t.Fatalf("failed to get discharge: %s", err)
	}
	if err = revoker.Revoke(cav.ID()); err != nil {
		t.Fatalf("failed to revoke: %s", err)
	}
	if discharge, err := discharger.Discharge(r.NewContext(), cav, impetus); err == nil || discharge != nil {
		t.Fatalf("got a discharge for a revoked caveat: %s", err)
	}
	if err = revoker.Revoke(cav.ID()); err != nil {
		t.Fatalf("failed to revoke again: %s", err)
	}
	if discharge, err := discharger.Discharge(r.NewContext(), cav, impetus); err == nil || discharge != nil {
		t.Fatalf("got a discharge for a doubly revoked caveat: %s", err)
	}
}
