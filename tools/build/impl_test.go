package main

import (
	"bytes"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/binary"
	"veyron.io/veyron/veyron2/services/mgmt/build"
	verror "veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/profiles"
)

type mock struct{}

func (mock) Build(ctx build.BuilderBuildContext, arch build.Architecture, opsys build.OperatingSystem) ([]byte, error) {
	vlog.VI(2).Infof("Build(%v, %v) was called", arch, opsys)
	iterator := ctx.RecvStream()
	for iterator.Advance() {
	}
	if err := iterator.Err(); err != nil {
		vlog.Errorf("Advance() failed: %v", err)
		return nil, verror.Make(verror.Internal, ctx)
	}
	return nil, nil
}

func (mock) Describe(_ ipc.ServerContext, name string) (binary.Description, error) {
	vlog.VI(2).Infof("Describe(%v) was called", name)
	return binary.Description{}, nil
}

type dispatcher struct{}

func startServer(runtime veyron2.Runtime, t *testing.T) (ipc.Server, naming.Endpoint) {
	server, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	endpoints, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
	unpublished := ""
	if err := server.Serve(unpublished, build.BuilderServer(&mock{}), nil); err != nil {
		t.Fatalf("Serve(%v) failed: %v", unpublished, err)
	}
	return server, endpoints[0]
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("Stop() failed: %v", err)
	}
}

func TestBuildClient(t *testing.T) {
	var err error
	runtime, err = rt.New()
	if err != nil {
		t.Fatalf("Unexpected error initializing runtime: %s", err)
	}
	server, endpoint := startServer(runtime, t)
	defer stopServer(t, server)

	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)

	// Test the 'Build' command.
	if err := cmd.Execute([]string{"build", naming.JoinAddressName(endpoint.String(), ""), "veyron.io/veyron/veyron/tools/build"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from build: got %q, expected %q", got, expected)
	}
}
