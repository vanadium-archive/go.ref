package impl_test

import (
	"bytes"
	"strings"
	"testing"

	"veyron/tools/mounttable/impl"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mounttable"
	"veyron2/vlog"
)

type server struct {
	suffix string
}

func (s *server) Glob(_ ipc.Context, pattern string, stream mounttable.GlobableServiceGlobStream) error {
	vlog.VI(2).Infof("Glob() was called. suffix=%v pattern=%q", s.suffix, pattern)
	stream.Send(mounttable.MountEntry{"name1", []mounttable.MountedServer{{"server1", 123}}})
	stream.Send(mounttable.MountEntry{"name2", []mounttable.MountedServer{{"server2", 456}, {"server3", 789}}})
	return nil
}

func (s *server) Mount(_ ipc.Context, server string, ttl uint32) error {
	vlog.VI(2).Infof("Mount() was called. suffix=%v server=%q ttl=%d", s.suffix, server, ttl)
	return nil
}

func (s *server) Unmount(_ ipc.Context, server string) error {
	vlog.VI(2).Infof("Unmount() was called. suffix=%v server=%q", s.suffix, server)
	return nil
}

func (s *server) ResolveStep(ipc.Context) (servers []mounttable.MountedServer, suffix string, err error) {
	vlog.VI(2).Infof("ResolveStep() was called. suffix=%v", s.suffix)
	servers = []mounttable.MountedServer{{"server1", 123}}
	suffix = s.suffix
	return
}

type dispatcher struct {
}

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
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

func TestMountTableClient(t *testing.T) {
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
	if expected, got := `Servers: [{server1 123}] Suffix: "name"`, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
