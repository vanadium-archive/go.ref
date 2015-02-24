package main

import (
	"bytes"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mounttable"
	"v.io/v23/services/security/access"
	"v.io/v23/vlog"

	"v.io/core/veyron/lib/testutil"
	_ "v.io/core/veyron/profiles"
)

type server struct {
	suffix string
}

func (s *server) Glob__(ctx ipc.ServerContext, pattern string) (<-chan naming.VDLGlobReply, error) {
	vlog.VI(2).Infof("Glob() was called. suffix=%v pattern=%q", s.suffix, pattern)
	ch := make(chan naming.VDLGlobReply, 2)
	ch <- naming.VDLGlobReplyEntry{naming.VDLMountEntry{"name1", []naming.VDLMountedServer{{"server1", nil, 123}}, false}}
	ch <- naming.VDLGlobReplyEntry{naming.VDLMountEntry{"name2", []naming.VDLMountedServer{{"server2", nil, 456}, {"server3", nil, 789}}, false}}
	close(ch)
	return ch, nil
}

func (s *server) Mount(_ ipc.ServerContext, server string, ttl uint32, flags naming.MountFlag) error {
	vlog.VI(2).Infof("Mount() was called. suffix=%v server=%q ttl=%d", s.suffix, server, ttl)
	return nil
}

func (s *server) MountX(_ ipc.ServerContext, server string, patterns []security.BlessingPattern, ttl uint32, flags naming.MountFlag) error {
	vlog.VI(2).Infof("MountX() was called. suffix=%v servers=%q patterns=%v ttl=%d", s.suffix, server, patterns, ttl)
	return nil
}

func (s *server) Unmount(_ ipc.ServerContext, server string) error {
	vlog.VI(2).Infof("Unmount() was called. suffix=%v server=%q", s.suffix, server)
	return nil
}

func (s *server) ResolveStep(ipc.ServerContext) (entry naming.VDLMountEntry, err error) {
	vlog.VI(2).Infof("ResolveStep() was called. suffix=%v", s.suffix)
	entry.Servers = []naming.VDLMountedServer{{"server1", nil, 123}}
	entry.Name = s.suffix
	return
}

func (s *server) ResolveStepX(ipc.ServerContext) (entry naming.VDLMountEntry, err error) {
	vlog.VI(2).Infof("ResolveStepX() was called. suffix=%v", s.suffix)
	entry.Servers = []naming.VDLMountedServer{{"server1", nil, 123}}
	entry.Name = s.suffix
	return
}

func (s *server) Delete(ipc.ServerContext, bool) error {
	vlog.VI(2).Infof("Delete() was called. suffix=%v", s.suffix)
	return nil
}
func (s *server) SetACL(ipc.ServerContext, access.TaggedACLMap, string) error {
	vlog.VI(2).Infof("SetACL() was called. suffix=%v", s.suffix)
	return nil
}

func (s *server) GetACL(ipc.ServerContext) (access.TaggedACLMap, string, error) {
	vlog.VI(2).Infof("GetACL() was called. suffix=%v", s.suffix)
	return nil, "", nil
}

type dispatcher struct {
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return mounttable.MountTableServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (ipc.Server, naming.Endpoint, error) {
	dispatcher := new(dispatcher)
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

	// Test the 'glob' command.
	if err := cmd.Execute([]string{"glob", naming.JoinAddressName(endpoint.String(), ""), "*"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "name1 server1 (TTL 2m3s)\nname2 server2 (TTL 7m36s) server3 (TTL 13m9s)", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'mount' command.
	if err := cmd.Execute([]string{"mount", "server", naming.JoinAddressName(endpoint.String(), ""), "123s"}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Name mounted successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'unmount' command.
	if err := cmd.Execute([]string{"unmount", "server", naming.JoinAddressName(endpoint.String(), "")}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Unmount successful or name not mounted.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'resolvestep' command.
	vlog.Infof("resovestep %s", naming.JoinAddressName(endpoint.String(), "name"))
	if err := cmd.Execute([]string{"resolvestep", naming.JoinAddressName(endpoint.String(), "name")}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := `Servers: [{server1 [] 123}] Suffix: "name" MT: false`, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
