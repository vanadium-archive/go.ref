package discharger

import (
	"os"
	"path/filepath"
	"testing"
	ssecurity "veyron/services/security"
	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
)

func revokerSetup(t *testing.T) (dischargerID security.PublicID, dischargerEndpoint, revokerEndpoint string, closeFunc func(), runtime veyron2.Runtime) {
	var revokerDirPath = filepath.Join(os.TempDir(), "revoker_dir")
	r := rt.Init()
	// Create and start revoker and revocation discharge service
	revokerServer, err := r.NewServer()
	if err != nil {
		t.Fatalf("rt.R().NewServer: %s", err)
	}
	revokerEP, err := revokerServer.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("revokerServer.Listen failed: %v", err)
	}
	revokerService, err := NewRevoker(revokerDirPath)
	if err != nil {
		t.Fatalf("NewRevoker failed: $v", err)
	}
	revokerServiceStub := ssecurity.NewServerRevoker(revokerService)
	err = revokerServer.Serve("", ipc.SoloDispatcher(revokerServiceStub, nil))
	if err != nil {
		t.Fatalf("revokerServer.Serve discharger: %s", err)
	}

	dischargerServer, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("rt.R().NewServer: %s", err)
	}
	dischargerEP, err := dischargerServer.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("revokerServer.Listen failed: %v", err)
	}
	dischargerServiceStub := ssecurity.NewServerDischarger(NewDischarger(r.Identity()))
	if err := dischargerServer.Serve("", ipc.SoloDispatcher(dischargerServiceStub, nil)); err != nil {
		t.Fatalf("revokerServer.Serve revoker: %s", err)
	}
	return r.Identity().PublicID(),
		naming.JoinAddressName(dischargerEP.String(), ""),
		naming.JoinAddressName(revokerEP.String(), ""),
		func() {
			defer os.RemoveAll(revokerDirPath)
			revokerServer.Stop()
			dischargerServer.Stop()
		},
		r
}

func TestDischargeRevokeDischargeRevokeDischarge(t *testing.T) {
	dcID, dc, rv, closeFunc, r := revokerSetup(t)
	defer closeFunc()
	revoker, err := ssecurity.BindRevoker(rv)
	if err != nil {
		t.Fatalf("error binding to server: ", err)
	}
	discharger, err := ssecurity.BindDischarger(dc)
	if err != nil {
		t.Fatalf("error binding to server: ", err)
	}

	preimage, cav, err := NewRevocationCaveat(dcID, dc)
	if err != nil {
		t.Fatalf("failed to create public key caveat: %s", err)
	}
	if _, err = discharger.Discharge(r.NewContext(), cav); err != nil {
		t.Fatalf("failed to get discharge: %s", err)
	}
	if err = revoker.Revoke(r.NewContext(), preimage); err != nil {
		t.Fatalf("failed to revoke: %s", err)
	}
	if discharge, err := discharger.Discharge(r.NewContext(), cav); err == nil || discharge != nil {
		t.Fatalf("got a discharge for a revoked caveat: %s", err)
	}
	if err = revoker.Revoke(r.NewContext(), preimage); err != nil {
		t.Fatalf("failed to revoke again: %s", err)
	}
	if discharge, err := discharger.Discharge(r.NewContext(), cav); err == nil || discharge != nil {
		t.Fatalf("got a discharge for a doubly revoked caveat: %s", err)
	}
}
