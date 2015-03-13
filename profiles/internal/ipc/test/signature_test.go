package test

import (
	"fmt"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/ipc/reserved"
	"v.io/v23/naming"
	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/test"
)

func startSigServer(ctx *context.T, sig sigImpl) (string, func(), error) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("failed to start sig server: %v", err)
	}
	eps, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen: %v", err)
	}
	if err := server.Serve("", sig, nil); err != nil {
		return "", nil, err
	}
	return eps[0].String(), func() { server.Stop() }, nil
}

type sigImpl struct{}

func (sigImpl) NonStreaming0(ipc.ServerCall) error                       { panic("X") }
func (sigImpl) NonStreaming1(_ ipc.ServerCall, _ string) (int64, error)  { panic("X") }
func (sigImpl) Streaming0(_ *streamStringBool) error                     { panic("X") }
func (sigImpl) Streaming1(_ *streamStringBool, _ int64) (float64, error) { panic("X") }

type streamStringBool struct{ ipc.StreamServerCall }

func (*streamStringBool) Init(ipc.StreamServerCall) { panic("X") }
func (*streamStringBool) RecvStream() interface {
	Advance() bool
	Value() string
	Err() error
} {
	panic("X")
}
func (*streamStringBool) SendStream() interface {
	Send(_ bool) error
} {
	panic("X")
}

func TestMethodSignature(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	ep, stop, err := startSigServer(ctx, sigImpl{})
	if err != nil {
		t.Fatalf("startSigServer: %v", err)
	}
	defer stop()
	name := naming.JoinAddressName(ep, "")

	tests := []struct {
		Method string
		Want   signature.Method
	}{
		{"NonStreaming0", signature.Method{
			Name: "NonStreaming0",
		}},
		{"NonStreaming1", signature.Method{
			Name:    "NonStreaming1",
			InArgs:  []signature.Arg{{Type: vdl.StringType}},
			OutArgs: []signature.Arg{{Type: vdl.Int64Type}},
		}},
		{"Streaming0", signature.Method{
			Name:      "Streaming0",
			InStream:  &signature.Arg{Type: vdl.StringType},
			OutStream: &signature.Arg{Type: vdl.BoolType},
		}},
		{"Streaming1", signature.Method{
			Name:      "Streaming1",
			InArgs:    []signature.Arg{{Type: vdl.Int64Type}},
			OutArgs:   []signature.Arg{{Type: vdl.Float64Type}},
			InStream:  &signature.Arg{Type: vdl.StringType},
			OutStream: &signature.Arg{Type: vdl.BoolType},
		}},
	}
	for _, test := range tests {
		sig, err := reserved.MethodSignature(ctx, name, test.Method)
		if err != nil {
			t.Errorf("call failed: %v", err)
		}
		if got, want := sig, test.Want; !reflect.DeepEqual(got, want) {
			t.Errorf("%s got %#v, want %#v", test.Method, got, want)
		}
	}
}

func TestSignature(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	ep, stop, err := startSigServer(ctx, sigImpl{})
	if err != nil {
		t.Fatalf("startSigServer: %v", err)
	}
	defer stop()
	name := naming.JoinAddressName(ep, "")
	sig, err := reserved.Signature(ctx, name)
	if err != nil {
		t.Errorf("call failed: %v", err)
	}
	if got, want := len(sig), 2; got != want {
		t.Fatalf("got sig %#v len %d, want %d", sig, got, want)
	}
	// Check expected methods.
	methods := signature.Interface{
		Doc: "The empty interface contains methods not attached to any interface.",
		Methods: []signature.Method{
			{
				Name: "NonStreaming0",
			},
			{
				Name:    "NonStreaming1",
				InArgs:  []signature.Arg{{Type: vdl.StringType}},
				OutArgs: []signature.Arg{{Type: vdl.Int64Type}},
			},
			{
				Name:      "Streaming0",
				InStream:  &signature.Arg{Type: vdl.StringType},
				OutStream: &signature.Arg{Type: vdl.BoolType},
			},
			{
				Name:      "Streaming1",
				InArgs:    []signature.Arg{{Type: vdl.Int64Type}},
				OutArgs:   []signature.Arg{{Type: vdl.Float64Type}},
				InStream:  &signature.Arg{Type: vdl.StringType},
				OutStream: &signature.Arg{Type: vdl.BoolType},
			},
		},
	}
	if got, want := sig[0], methods; !reflect.DeepEqual(got, want) {
		t.Errorf("got sig[0] %#v, want %#v", got, want)
	}
	// Check reserved methods.
	if got, want := sig[1].Name, "__Reserved"; got != want {
		t.Errorf("got sig[1].Name %q, want %q", got, want)
	}
	if got, want := signature.MethodNames(sig[1:2]), []string{"__Glob", "__MethodSignature", "__Signature"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got sig[1] methods %v, want %v", got, want)
	}
}
