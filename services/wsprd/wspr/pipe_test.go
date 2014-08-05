package wspr

import (
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

func TestEncodeDecodeIdentity(t *testing.T) {
	identity := security.FakePrivateID("/fake/private/id")
	resultIdentity := decodeIdentity(r.Logger(), encodeIdentity(r.Logger(), identity))
	if identity != resultIdentity {
		t.Errorf("expected decodeIdentity(encodeIdentity(identity)) to be %v, got %v", identity, resultIdentity)
	}
}
