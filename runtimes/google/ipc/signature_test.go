package ipc_test

import (
	"fmt"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/reserved"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/profiles"
)

func init() { testutil.Init() }

func startSigServer(rt veyron2.Runtime, sig sigImpl) (string, func(), error) {
	server, err := rt.NewServer()
	if err != nil {
		return "", nil, fmt.Errorf("failed to start sig server: %v", err)
	}
	ep, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen: %v", err)
	}
	if err := server.Serve("", sig, nil); err != nil {
		return "", nil, err
	}
	return ep.String(), func() { server.Stop() }, nil
}

type sigImpl struct{}

func (sigImpl) NonStreaming0(ipc.ServerContext) {}

// TODO(toddw): This test doesn't work yet, because we try to send *vdl.Type
// back over the wire for the new Signature format, but that depends on vom2
// support for typeobject and encoding *vdl.Type.  Re-enable this test when we
// have vom2 optionally enabled in our stack.
func disabledTestMethodSignature(t *testing.T) {
	runtime := rt.Init()
	defer runtime.Cleanup()

	ep, stop, err := startSigServer(runtime, sigImpl{})
	if err != nil {
		t.Fatalf("startSigServer: %v", err)
	}
	defer stop()

	tests := []struct {
		Method string
		Want   ipc.MethodSig
	}{
		{"NonStreaming0", ipc.MethodSig{
			Name: "NonStreaming0",
		}},
	}
	for _, test := range tests {
		name := naming.JoinAddressName(ep, "")
		sig, err := reserved.MethodSignature(rt.R().NewContext(), name, test.Method)
		if err != nil {
			t.Errorf("call failed: %v", err)
		}
		if got, want := sig, test.Want; !reflect.DeepEqual(got, want) {
			t.Errorf("%s got %#v, want %#v", test.Method, got, want)
		}
	}
}
