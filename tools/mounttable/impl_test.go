package main

import (
	"bytes"
	"strings"
	"testing"

	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/naming"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/security"
	"v.io/veyron/veyron2/services/mounttable"
	"v.io/veyron/veyron2/vlog"

	"v.io/veyron/veyron/profiles"
)

type server struct {
	suffix string
}

func (s *server) Glob__(ctx ipc.ServerContext, pattern string) (<-chan naming.VDLMountEntry, error) {
	vlog.VI(2).Infof("Glob() was called. suffix=%v pattern=%q", s.suffix, pattern)
	ch := make(chan naming.VDLMountEntry, 2)
	ch <- naming.VDLMountEntry{"name1", []naming.VDLMountedServer{{"server1", 123}}, false}
	ch <- naming.VDLMountEntry{"name2", []naming.VDLMountedServer{{"server2", 456}, {"server3", 789}}, false}
	close(ch)
	return ch, nil
}

func (s *server) Mount(_ ipc.ServerContext, server string, ttl uint32, flags naming.MountFlag) error {
	vlog.VI(2).Infof("Mount() was called. suffix=%v server=%q ttl=%d", s.suffix, server, ttl)
	return nil
}

func (s *server) Unmount(_ ipc.ServerContext, server string) error {
	vlog.VI(2).Infof("Unmount() was called. suffix=%v server=%q", s.suffix, server)
	return nil
}

func (s *server) ResolveStep(ipc.ServerContext) (servers []naming.VDLMountedServer, suffix string, err error) {
	vlog.VI(2).Infof("ResolveStep() was called. suffix=%v", s.suffix)
	servers = []naming.VDLMountedServer{{"server1", 123}}
	suffix = s.suffix
	return
}

func (s *server) ResolveStepX(ipc.ServerContext) (entry naming.VDLMountEntry, err error) {
	vlog.VI(2).Infof("ResolveStepX() was called. suffix=%v", s.suffix)
	entry.Servers = []naming.VDLMountedServer{{"server1", 123}}
	entry.Name = s.suffix
	return
}

type dispatcher struct {
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return mounttable.MountTableServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	dispatcher := new(dispatcher)
	server, err := r.NewServer()
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	endpoints, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		return nil, nil, err
	}
	if err := server.ServeDispatcher("", dispatcher); err != nil {
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

func TestMountTableClient(t *testing.T) {
	var err error
	runtime, err = rt.New()
	if err != nil {
		t.Fatalf("Unexpected error initializing runtime: %s", err)
	}
	defer runtime.Cleanup()

	server, endpoint, err := startServer(t, runtime)
	if err != nil {
		return
	}
	defer stopServer(t, server)
	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)

	// Test the 'glob' command.
	if err := cmd.Execute([]string{"glob", naming.JoinAddressName(endpoint.String(), ""), "*"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "name1 server1 (TTL 2m3s)\nname2 server2 (TTL 7m36s) server3 (TTL 13m9s)", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'mount' command.
	if err := cmd.Execute([]string{"mount", naming.JoinAddressName(endpoint.String(), ""), "server", "123s"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Name mounted successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'unmount' command.
	if err := cmd.Execute([]string{"unmount", naming.JoinAddressName(endpoint.String(), ""), "server"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Name unmounted successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'resolvestep' command.
	vlog.Infof("resovestep %s", naming.JoinAddressName(endpoint.String(), "name"))
	if err := cmd.Execute([]string{"resolvestep", naming.JoinAddressName(endpoint.String(), "name")}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := `Servers: [{server1 123}] Suffix: "name" MT: false`, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
