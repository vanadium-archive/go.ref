package revocation

import (
	"os"
	"path/filepath"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"

	"veyron.io/veyron/veyron/profiles"
	services "veyron.io/veyron/veyron/services/security"
	"veyron.io/veyron/veyron/services/security/discharger"
)

func revokerSetup(t *testing.T) (dischargerID security.PublicID, dischargerEndpoint string, revoker *RevocationManager, closeFunc func(), runtime veyron2.Runtime) {
	var dir = filepath.Join(os.TempDir(), "revoker_test_dir")
	r := rt.Init()
	revokerService, err := NewRevocationManager(dir)
	if err != nil {
		t.Fatalf("NewRevocationManager failed: %v", err)
	}

	dischargerServer, err := r.NewServer()
	if err != nil {
		t.Fatalf("rt.R().NewServer: %s", err)
	}
	dischargerEP, err := dischargerServer.ListenX(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("dischargerServer.Listen failed: %v", err)
	}
	dischargerServiceStub := services.NewServerDischarger(discharger.NewDischarger(r.Identity()))
	if err := dischargerServer.Serve("", ipc.LeafDispatcher(dischargerServiceStub, nil)); err != nil {
		t.Fatalf("dischargerServer.Serve revoker: %s", err)
	}
	return r.Identity().PublicID(),
		naming.JoinAddressName(dischargerEP.String(), ""),
		revokerService,
		func() {
			defer os.RemoveAll(dir)
			dischargerServer.Stop()
		},
		r
}

func TestDischargeRevokeDischargeRevokeDischarge(t *testing.T) {
	dcID, dc, revoker, closeFunc, r := revokerSetup(t)
	defer closeFunc()

	discharger, err := services.BindDischarger(dc)
	if err != nil {
		t.Fatalf("error binding to server: ", err)
	}

	cav, err := revoker.NewCaveat(dcID, dc)
	if err != nil {
		t.Fatalf("failed to create public key caveat: %s", err)
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
