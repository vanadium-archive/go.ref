package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mounttable"
	"v.io/v23/services/security/access"
	vdltime "v.io/v23/vdlroot/time"
	"v.io/x/lib/vlog"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/test"
)

var (
	now       = time.Now()
	deadline1 = vdltime.Deadline{now.Add(time.Minute * 1)}
	deadline2 = vdltime.Deadline{now.Add(time.Minute * 2)}
	deadline3 = vdltime.Deadline{now.Add(time.Minute * 3)}
)

type server struct {
	suffix string
}

func (s *server) Glob__(call ipc.ServerCall, pattern string) (<-chan naming.GlobReply, error) {
	vlog.VI(2).Infof("Glob() was called. suffix=%v pattern=%q", s.suffix, pattern)
	ch := make(chan naming.GlobReply, 2)
	ch <- naming.GlobReplyEntry{naming.MountEntry{"name1", []naming.MountedServer{{"server1", nil, deadline1}}, false}}
	ch <- naming.GlobReplyEntry{naming.MountEntry{"name2", []naming.MountedServer{{"server2", nil, deadline2}, {"server3", nil, deadline3}}, false}}
	close(ch)
	return ch, nil
}

func (s *server) Mount(_ ipc.ServerCall, server string, ttl uint32, flags naming.MountFlag) error {
	vlog.VI(2).Infof("Mount() was called. suffix=%v server=%q ttl=%d", s.suffix, server, ttl)
	return nil
}

func (s *server) MountX(_ ipc.ServerCall, server string, patterns []security.BlessingPattern, ttl uint32, flags naming.MountFlag) error {
	vlog.VI(2).Infof("MountX() was called. suffix=%v servers=%q patterns=%v ttl=%d", s.suffix, server, patterns, ttl)
	return nil
}

func (s *server) Unmount(_ ipc.ServerCall, server string) error {
	vlog.VI(2).Infof("Unmount() was called. suffix=%v server=%q", s.suffix, server)
	return nil
}

func (s *server) ResolveStep(ipc.ServerCall) (entry naming.MountEntry, err error) {
	vlog.VI(2).Infof("ResolveStep() was called. suffix=%v", s.suffix)
	entry.Servers = []naming.MountedServer{{"server1", nil, deadline1}}
	entry.Name = s.suffix
	return
}

func (s *server) ResolveStepX(ipc.ServerCall) (entry naming.MountEntry, err error) {
	vlog.VI(2).Infof("ResolveStepX() was called. suffix=%v", s.suffix)
	entry.Servers = []naming.MountedServer{{"server1", nil, deadline1}}
	entry.Name = s.suffix
	return
}

func (s *server) Delete(ipc.ServerCall, bool) error {
	vlog.VI(2).Infof("Delete() was called. suffix=%v", s.suffix)
	return nil
}
func (s *server) SetPermissions(ipc.ServerCall, access.Permissions, string) error {
	vlog.VI(2).Infof("SetPermissions() was called. suffix=%v", s.suffix)
	return nil
}

func (s *server) GetPermissions(ipc.ServerCall) (access.Permissions, string, error) {
	vlog.VI(2).Infof("GetPermissions() was called. suffix=%v", s.suffix)
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
	gctx, shutdown = test.InitForTest()
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
	const deadRE = `\(Deadline ([^)]+)\)`
	if got, wantRE := strings.TrimSpace(stdout.String()), regexp.MustCompile("name1 server1 "+deadRE+"\nname2 server2 "+deadRE+" server3 "+deadRE); !wantRE.MatchString(got) {
		t.Errorf("got %q, want regexp %q", got, wantRE)
	}
	stdout.Reset()

	// Test the 'mount' command.
	if err := cmd.Execute([]string{"mount", "server", naming.JoinAddressName(endpoint.String(), ""), "123s"}); err != nil {
		t.Fatalf("%v", err)
	}
	if got, want := strings.TrimSpace(stdout.String()), "Name mounted successfully."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	stdout.Reset()

	// Test the 'unmount' command.
	if err := cmd.Execute([]string{"unmount", "server", naming.JoinAddressName(endpoint.String(), "")}); err != nil {
		t.Fatalf("%v", err)
	}
	if got, want := strings.TrimSpace(stdout.String()), "Unmount successful or name not mounted."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	stdout.Reset()

	// Test the 'resolvestep' command.
	vlog.Infof("resovestep %s", naming.JoinAddressName(endpoint.String(), "name"))
	if err := cmd.Execute([]string{"resolvestep", naming.JoinAddressName(endpoint.String(), "name")}); err != nil {
		t.Fatalf("%v", err)
	}
	if got, wantRE := strings.TrimSpace(stdout.String()), regexp.MustCompile(`Servers: \[\{server1 \[\] [^}]+\}\] Suffix: "name" MT: false`); !wantRE.MatchString(got) {
		t.Errorf("got %q, want regexp %q", got, wantRE)
	}
	stdout.Reset()
}
