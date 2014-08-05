package wspr

import (
	"encoding/json"
	"testing"
	"veyron/services/wsprd/lib"
	"veyron2"
	"veyron2/rt"
	"veyron2/security"
)

var r veyron2.Runtime

func init() {
	r = rt.Init()
}

type testWriter struct{}

func (*testWriter) Send(lib.ResponseType, interface{}) error { return nil }
func (*testWriter) Error(error)                              {}

func TestHandleAssocIdentity(t *testing.T) {
	wspr := NewWSPR(0, "mockVeyronProxyEP")
	p := pipe{wspr: wspr, logger: wspr.rt.Logger()}
	p.setup()

	privateId := security.FakePrivateID("/fake/private/id")
	identityData := assocIdentityData{
		Account:  "test@example.org",
		Identity: encodeIdentity(wspr.logger, privateId),
		Origin:   "my.webapp.com",
	}
	jsonIdentityDataBytes, err := json.Marshal(identityData)
	if err != nil {
		t.Errorf("json.Marshal(%v) failed: %v", identityData, err)
	}
	jsonIdentityData := string(jsonIdentityDataBytes)
	writer := testWriter{}
	p.handleAssocIdentity(jsonIdentityData, lib.ClientWriter(&writer))
	// Check that the pipe has the privateId
	if p.controller.RT().Identity() != privateId {
		t.Errorf("p.privateId was not set. got: %v, expected: %v", p.controller.RT().Identity(), identityData.Identity)
	}
	// Check that wspr idManager has the origin
	_, err = wspr.idManager.Identity(identityData.Origin)
	if err != nil {
		t.Errorf("wspr.idManager.Identity(%v) failed: %v", identityData.Origin, err)
	}
}

func TestEncodeDecodeIdentity(t *testing.T) {
	identity := security.FakePrivateID("/fake/private/id")
	resultIdentity := decodeIdentity(r.Logger(), encodeIdentity(r.Logger(), identity))
	if identity != resultIdentity {
		t.Errorf("expected decodeIdentity(encodeIdentity(identity)) to be %v, got %v", identity, resultIdentity)
	}
}
