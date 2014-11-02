package main

import (
	"bytes"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mounttable"
	"veyron.io/veyron/veyron2/services/mounttable/types"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/profiles"
)

type server struct {
	suffix string
}

func (s *server) Glob(_ ipc.ServerContext, pattern string, stream mounttable.GlobbableServiceGlobStream) error {
	vlog.VI(2).Infof("Glob() was called. suffix=%v pattern=%q", s.suffix, pattern)
	sender := stream.SendStream()
	sender.Send(types.MountEntry{"name1", []types.MountedServer{{"server1", 123}}, false})
	sender.Send(types.MountEntry{"name2", []types.MountedServer{{"server2", 456}, {"server3", 789}}, false})
	return nil
}

func (s *server) Mount(_ ipc.ServerContext, server string, ttl uint32, flags types.MountFlag) error {
	vlog.VI(2).Infof("Mount() was called. suffix=%v server=%q ttl=%d", s.suffix, server, ttl)
	return nil
}

func (s *server) Unmount(_ ipc.ServerContext, server string) error {
	vlog.VI(2).Infof("Unmount() was called. suffix=%v server=%q", s.suffix, server)
	return nil
}

func (s *server) ResolveStep(ipc.ServerContext) (servers []types.MountedServer, suffix string, err error) {
	vlog.VI(2).Infof("ResolveStep() was called. suffix=%v", s.suffix)
	servers = []types.MountedServer{{"server1", 123}}
	suffix = s.suffix
	return
}

func (s *server) ResolveStepX(ipc.ServerContext) (entry types.MountEntry, err error) {
	vlog.VI(2).Infof("ResolveStepX() was called. suffix=%v", s.suffix)
	entry.Servers = []types.MountedServer{{"server1", 123}}
	entry.Name = s.suffix
	return
}

type dispatcher struct {
}

func (d *dispatcher) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(mounttable.NewServerMountTable(&server{suffix: suffix}))
	return invoker, nil, nil
}

func startServer(t *testing.T, r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	dispatcher := new(dispatcher)
	server, err := r.NewServer()
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	endpoint, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		return nil, nil, err
	}
	if err := server.Serve("", dispatcher); err != nil {
		t.Errorf("Serve failed: %v", err)
		return nil, nil, err
	}
	return server, endpoint, nil
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestMountTableClient(t *testing.T) {
	runtime := rt.Init()
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
	if err := cmd.Execute([]string{"resolvestep", naming.JoinAddressName(endpoint.String(), "//name")}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := `Servers: [{server1 123}] Suffix: "name" MT: false`, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
