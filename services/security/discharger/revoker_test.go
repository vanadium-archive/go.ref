package discharger

import (
	"testing"
	ssecurity "veyron/services/security"
	teststore "veyron/services/store/testutil"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
)

func init() {
	rt.Init()
}

func setup(t *testing.T) (dischargerID security.PublicID, dischargerEndpoint, revokerEndpoint string, closeFunc func()) {
	// Create and start the store instance that the revoker will use
	storeServer, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("rt.R().NewServer: %s", err)
	}
	storeVeyronName, closeStore := teststore.NewStore(t, storeServer, rt.R().Identity().PublicID())

	// Create and start revoker and revocation discharge service
	revokerServer, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("rt.R().NewServer: %s", err)
	}
	revokerEP, err := revokerServer.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("revokerServer.Listen failed: %v", err)
	}
	revokerService, err := NewRevoker(storeVeyronName, "/testrevoker")
	if err != nil {
		t.Fatalf("setup revoker service: %s", err)
	}
	err = revokerServer.Serve("", ipc.SoloDispatcher(revokerService, nil))
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
	err = dischargerServer.Serve("", ipc.SoloDispatcher(New(rt.R().Identity()), nil))
	if err != nil {
		t.Fatalf("revokerServer.Serve revoker: %s", err)
	}
	return rt.R().Identity().PublicID(),
		naming.JoinAddressName(dischargerEP.String(), ""),
		naming.JoinAddressName(revokerEP.String(), ""),
		func() {
			revokerServer.Stop()
			dischargerServer.Stop()
			closeStore()
		}
}

func TestDischargeRevokeDischargeRevokeDischarge(t *testing.T) {
	dcID, dc, rv, closeFunc := setup(t)
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

	if _, err = discharger.Discharge(rt.R().NewContext(), cav); err != nil {
		t.Fatalf("failed to get discharge: %s", err)
	}
	if err = revoker.Revoke(rt.R().NewContext(), preimage); err != nil {
		t.Fatalf("failed to revoke: %s", err)
	}
	if discharge, err := discharger.Discharge(rt.R().NewContext(), cav); err == nil || discharge != nil {
		t.Fatalf("got a discharge for a revoked caveat: %s", err)
	}
	if err = revoker.Revoke(rt.R().NewContext(), preimage); err != nil {
		t.Fatalf("failed to revoke again: %s", err)
	}
	if discharge, err := discharger.Discharge(rt.R().NewContext(), cav); err == nil || discharge != nil {
		t.Fatalf("got a discharge for a doubly revoked caveat: %s", err)
	}
}
