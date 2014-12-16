package revocation

import (
	"bytes"
	"testing"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"

	"veyron.io/veyron/veyron/profiles"
	services "veyron.io/veyron/veyron/services/security"
	"veyron.io/veyron/veyron/services/security/discharger"
)

type mockDatabase struct {
	tpCavIDToRevCavID   map[string][]byte
	revCavIDToTimestamp map[string]*time.Time
}

func (m *mockDatabase) InsertCaveat(thirdPartyCaveatID string, revocationCaveatID []byte) error {
	m.tpCavIDToRevCavID[thirdPartyCaveatID] = revocationCaveatID
	return nil
}

func (m *mockDatabase) Revoke(thirdPartyCaveatID string) error {
	timestamp := time.Now()
	m.revCavIDToTimestamp[string(m.tpCavIDToRevCavID[thirdPartyCaveatID])] = &timestamp
	return nil
}

func (m *mockDatabase) IsRevoked(revocationCaveatID []byte) (bool, error) {
	_, exists := m.revCavIDToTimestamp[string(revocationCaveatID)]
	return exists, nil
}

func (m *mockDatabase) RevocationTime(thirdPartyCaveatID string) (*time.Time, error) {
	return m.revCavIDToTimestamp[string(m.tpCavIDToRevCavID[thirdPartyCaveatID])], nil
}

func newRevocationManager(t *testing.T) *RevocationManager {
	revocationDB = &mockDatabase{make(map[string][]byte), make(map[string]*time.Time)}
	return &RevocationManager{}
}

func revokerSetup(t *testing.T, r veyron2.Runtime) (dischargerKey security.PublicKey, dischargerEndpoint string, revoker *RevocationManager, closeFunc func(), runtime veyron2.Runtime) {
	revokerService := newRevocationManager(t)
	dischargerServer, err := r.NewServer()
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
	return r.Principal().PublicKey(),
		naming.JoinAddressName(dischargerEPs[0].String(), ""),
		revokerService,
		func() {
			dischargerServer.Stop()
		},
		r
}

func TestDischargeRevokeDischargeRevokeDischarge(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %v", err)
	}
	defer r.Cleanup()

	dcKey, dc, revoker, closeFunc, r := revokerSetup(t, r)
	defer closeFunc()

	discharger := services.DischargerClient(dc)
	caveat, err := revoker.NewCaveat(dcKey, dc)
	if err != nil {
		t.Fatalf("failed to create revocation caveat: %s", err)
	}
	var cav security.ThirdPartyCaveat
	if err := vom.NewDecoder(bytes.NewBuffer(caveat.ValidatorVOM)).Decode(&cav); err != nil {
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
