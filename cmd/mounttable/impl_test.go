// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/mounttable"
	"v.io/v23/services/security/access"
	vdltime "v.io/v23/vdlroot/time"
	"v.io/x/lib/vlog"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/test"
)

var now = time.Now()

func init() {
	test.Init()
}

func deadline(minutes int) vdltime.Deadline {
	return vdltime.Deadline{now.Add(time.Minute * time.Duration(minutes))}
}

type server struct {
	suffix string
}

func (s *server) Glob__(call rpc.ServerCall, pattern string) (<-chan naming.GlobReply, error) {
	vlog.VI(2).Infof("Glob() was called. suffix=%v pattern=%q", s.suffix, pattern)
	ch := make(chan naming.GlobReply, 2)
	ch <- naming.GlobReplyEntry{naming.MountEntry{"name1", []naming.MountedServer{{"server1", deadline(1)}}, false, false}}
	ch <- naming.GlobReplyEntry{naming.MountEntry{"name2", []naming.MountedServer{{"server2", deadline(2)}, {"server3", deadline(3)}}, false, false}}
	close(ch)
	return ch, nil
}

func (s *server) Mount(_ rpc.ServerCall, server string, ttl uint32, flags naming.MountFlag) error {
	vlog.VI(2).Infof("Mount() was called. suffix=%v server=%q ttl=%d", s.suffix, server, ttl)
	return nil
}

// DEPRECATED: Remove before release.
func (s *server) MountX(_ rpc.ServerCall, server string, patterns []security.BlessingPattern, ttl uint32, flags naming.MountFlag) error {
	return fmt.Errorf("MountX should not have been called")
}

func (s *server) Unmount(_ rpc.ServerCall, server string) error {
	vlog.VI(2).Infof("Unmount() was called. suffix=%v server=%q", s.suffix, server)
	return nil
}

func (s *server) ResolveStep(rpc.ServerCall) (entry naming.MountEntry, err error) {
	vlog.VI(2).Infof("ResolveStep() was called. suffix=%v", s.suffix)
	entry.Servers = []naming.MountedServer{{"server1", deadline(1)}}
	entry.Name = s.suffix
	return
}

func (s *server) ResolveStepX(rpc.ServerCall) (entry naming.MountEntry, err error) {
	vlog.VI(2).Infof("ResolveStepX() was called. suffix=%v", s.suffix)
	entry.Servers = []naming.MountedServer{{"server1", deadline(1)}}
	entry.Name = s.suffix
	return
}

func (s *server) Delete(rpc.ServerCall, bool) error {
	vlog.VI(2).Infof("Delete() was called. suffix=%v", s.suffix)
	return nil
}
func (s *server) SetPermissions(rpc.ServerCall, access.Permissions, string) error {
	vlog.VI(2).Infof("SetPermissions() was called. suffix=%v", s.suffix)
	return nil
}

func (s *server) GetPermissions(rpc.ServerCall) (access.Permissions, string, error) {
	vlog.VI(2).Infof("GetPermissions() was called. suffix=%v", s.suffix)
	return nil, "", nil
}

type dispatcher struct {
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return mounttable.MountTableServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (rpc.Server, naming.Endpoint, error) {
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

func stopServer(t *testing.T, server rpc.Server) {
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

	// Make sure to use our newly created mounttable rather than the
	// default.
	v23.GetNamespace(gctx).SetRoots(endpoint.Name())

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
	if err := cmd.Execute([]string{"mount", "server", endpoint.Name(), "123s"}); err != nil {
		t.Fatalf("%v", err)
	}
	if got, want := strings.TrimSpace(stdout.String()), "Name mounted successfully."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	stdout.Reset()

	// Test the 'unmount' command.
	if err := cmd.Execute([]string{"unmount", "server", endpoint.Name()}); err != nil {
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
	if got, wantRE := strings.TrimSpace(stdout.String()), regexp.MustCompile(`Servers: \[\{server1 [^}]+\}\] Suffix: "name" MT: false`); !wantRE.MatchString(got) {
		t.Errorf("got %q, want regexp %q", got, wantRE)
	}
	stdout.Reset()
}
