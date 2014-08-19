package impl_test

import (
	"bytes"
	"strings"
	"testing"

	"veyron/tools/build/impl"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/binary"
	"veyron2/services/mgmt/build"
	"veyron2/verror"
	"veyron2/vlog"
)

var errInternalError = verror.Internalf("internal error")

type mock struct{}

func (mock) Build(_ ipc.ServerContext, arch build.Architecture, opsys build.OperatingSystem, stream build.BuilderServiceBuildStream) ([]byte, error) {
	vlog.VI(2).Infof("Build(%v, %v) was called", arch, opsys)
	iterator := stream.RecvStream()
	for iterator.Advance() {
	}
	if err := iterator.Err(); err != nil {
		vlog.Errorf("Advance() failed: %v", err)
		return nil, errInternalError
	}
	return nil, nil
}

func (mock) Describe(_ ipc.ServerContext, name string) (binary.Description, error) {
	vlog.VI(2).Infof("Describe(%v) was called", name)
	return binary.Description{}, nil
}

type dispatcher struct{}

func startServer(t *testing.T) (ipc.Server, naming.Endpoint) {
	server, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	protocol, address := "tcp", "127.0.0.1:0"
	endpoint, err := server.Listen(protocol, address)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, address, err)
	}
	unpublished := ""
	if err := server.Serve(unpublished, ipc.LeafDispatcher(build.NewServerBuilder(&mock{}), nil)); err != nil {
		t.Fatalf("Serve(%v) failed: %v", unpublished, err)
	}
	return server, endpoint
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("Stop() failed: %v", err)
	}
}

func TestBuildClient(t *testing.T) {
	rt.Init()
	server, endpoint := startServer(t)
	defer stopServer(t, server)

	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)

	// Test the 'Build' command.
	if err := cmd.Execute([]string{"build", naming.JoinAddressName(endpoint.String(), ""), "veyron/tools/build"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from build: got %q, expected %q", got, expected)
	}
}
