package impl_test

import (
	"bytes"
	"strings"
	"testing"

	"veyron/tools/proximity/impl"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/proximity"
	"veyron2/vlog"
)

type server struct {
}

func (s *server) RegisterName(_ ipc.Context, name string) error {
	vlog.VI(2).Infof("RegisterName(%q) was called", name)
	return nil
}

func (s *server) UnregisterName(_ ipc.Context, name string) error {
	vlog.VI(2).Infof("UnregisterName(%q) was called", name)
	return nil
}

func (s *server) NearbyDevices(_ ipc.Context) ([]proximity.Device, error) {
	vlog.VI(2).Info("NearbyDevices() was called")
	devices := []proximity.Device{
		{MAC: "xx:xx:xx:xx:xx:xx", Names: []string{"name1", "name2"}, Distance: "1m"},
		{MAC: "yy:yy:yy:yy:yy:yy", Names: []string{"name3"}, Distance: "2m"},
	}
	return devices, nil
}

type dispatcher struct {
}

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(proximity.NewServerProximity(&server{}))
	return invoker, nil, nil
}

func startServer(t *testing.T, r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	dispatcher := &dispatcher{}
	server, err := r.NewServer()
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	if err := server.Register("", dispatcher); err != nil {
		t.Errorf("Register failed: %v", err)
		return nil, nil, err
	}
	endpoint, err := server.Listen("tcp", "localhost:0")
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		return nil, nil, err
	}
	return server, endpoint, nil
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestProximityClient(t *testing.T) {
	runtime := rt.Init()
	server, endpoint, err := startServer(t, runtime)
	if err != nil {
		return
	}
	defer stopServer(t, server)
	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	address := naming.JoinAddressName(endpoint.String(), "")

	// Test the 'register' command.
	if err := cmd.Execute([]string{"register", address, "myname"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Name registered successfully", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'unregister' command.
	if err := cmd.Execute([]string{"unregister", address, "myname"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Name unregistered successfully", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'nearbydevices' command.
	if err := cmd.Execute([]string{"nearbydevices", address}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Nearby Devices:\n0: MAC=xx:xx:xx:xx:xx:xx Names=[name1 name2] Distance=1m\n1: MAC=yy:yy:yy:yy:yy:yy Names=[name3] Distance=2m", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
