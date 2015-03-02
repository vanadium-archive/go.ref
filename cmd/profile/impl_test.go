package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/build"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/testutil"
	_ "v.io/x/ref/profiles"
	"v.io/x/ref/services/mgmt/profile"
	"v.io/x/ref/services/mgmt/repository"
)

var (
	// spec is an example profile specification used throughout the test.
	spec = profile.Specification{
		Arch:        build.AMD64,
		Description: "Example profile to test the profile repository implementation.",
		Format:      build.ELF,
		Libraries:   map[profile.Library]struct{}{profile.Library{Name: "foo", MajorVersion: "1", MinorVersion: "0"}: struct{}{}},
		Label:       "example",
		OS:          build.Linux,
	}
)

type server struct {
	suffix string
}

func (s *server) Label(ipc.ServerCall) (string, error) {
	vlog.VI(2).Infof("%v.Label() was called", s.suffix)
	if s.suffix != "exists" {
		return "", fmt.Errorf("profile doesn't exist: %v", s.suffix)
	}
	return spec.Label, nil
}

func (s *server) Description(ipc.ServerCall) (string, error) {
	vlog.VI(2).Infof("%v.Description() was called", s.suffix)
	if s.suffix != "exists" {
		return "", fmt.Errorf("profile doesn't exist: %v", s.suffix)
	}
	return spec.Description, nil
}

func (s *server) Specification(ipc.ServerCall) (profile.Specification, error) {
	vlog.VI(2).Infof("%v.Specification() was called", s.suffix)
	if s.suffix != "exists" {
		return profile.Specification{}, fmt.Errorf("profile doesn't exist: %v", s.suffix)
	}
	return spec, nil
}

func (s *server) Put(_ ipc.ServerCall, _ profile.Specification) error {
	vlog.VI(2).Infof("%v.Put() was called", s.suffix)
	return nil
}

func (s *server) Remove(ipc.ServerCall) error {
	vlog.VI(2).Infof("%v.Remove() was called", s.suffix)
	if s.suffix != "exists" {
		return fmt.Errorf("profile doesn't exist: %v", s.suffix)
	}
	return nil
}

type dispatcher struct {
}

func NewDispatcher() ipc.Dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.ProfileServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (ipc.Server, naming.Endpoint, error) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	endpoints, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		return nil, nil, err
	}
	if err := server.ServeDispatcher("", NewDispatcher()); err != nil {
		t.Errorf("ServeDispatcher failed: %v", err)
		return nil, nil, err
	}
	return server, endpoints[0], nil
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestProfileClient(t *testing.T) {
	var shutdown v23.Shutdown
	gctx, shutdown = testutil.InitForTest()
	defer shutdown()

	server, endpoint, err := startServer(t, gctx)
	if err != nil {
		return
	}
	defer stopServer(t, server)
	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	exists := naming.JoinAddressName(endpoint.String(), "exists")

	// Test the 'label' command.
	if err := cmd.Execute([]string{"label", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := spec.Label, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'description' command.
	if err := cmd.Execute([]string{"description", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := spec.Description, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'spec' command.
	if err := cmd.Execute([]string{"specification", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := fmt.Sprintf("%#v", spec), strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'put' command.
	if err := cmd.Execute([]string{"put", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Profile added successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'remove' command.
	if err := cmd.Execute([]string{"remove", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Profile removed successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
