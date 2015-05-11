// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/build"

	"v.io/x/lib/cmdline2"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/profile"
	"v.io/x/ref/services/repository"
	"v.io/x/ref/test"
)

var (
	// spec is an example profile specification used throughout the test.
	spec = profile.Specification{
		Arch:        build.ArchitectureAmd64,
		Description: "Example profile to test the profile repository implementation.",
		Format:      build.FormatElf,
		Libraries:   map[profile.Library]struct{}{profile.Library{Name: "foo", MajorVersion: "1", MinorVersion: "0"}: struct{}{}},
		Label:       "example",
		Os:          build.OperatingSystemLinux,
	}
)

//go:generate v23 test generate

type server struct {
	suffix string
}

func (s *server) Label(*context.T, rpc.ServerCall) (string, error) {
	vlog.VI(2).Infof("%v.Label() was called", s.suffix)
	if s.suffix != "exists" {
		return "", fmt.Errorf("profile doesn't exist: %v", s.suffix)
	}
	return spec.Label, nil
}

func (s *server) Description(*context.T, rpc.ServerCall) (string, error) {
	vlog.VI(2).Infof("%v.Description() was called", s.suffix)
	if s.suffix != "exists" {
		return "", fmt.Errorf("profile doesn't exist: %v", s.suffix)
	}
	return spec.Description, nil
}

func (s *server) Specification(*context.T, rpc.ServerCall) (profile.Specification, error) {
	vlog.VI(2).Infof("%v.Specification() was called", s.suffix)
	if s.suffix != "exists" {
		return profile.Specification{}, fmt.Errorf("profile doesn't exist: %v", s.suffix)
	}
	return spec, nil
}

func (s *server) Put(_ *context.T, _ rpc.ServerCall, _ profile.Specification) error {
	vlog.VI(2).Infof("%v.Put() was called", s.suffix)
	return nil
}

func (s *server) Remove(*context.T, rpc.ServerCall) error {
	vlog.VI(2).Infof("%v.Remove() was called", s.suffix)
	if s.suffix != "exists" {
		return fmt.Errorf("profile doesn't exist: %v", s.suffix)
	}
	return nil
}

type dispatcher struct {
}

func NewDispatcher() rpc.Dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.ProfileServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (rpc.Server, naming.Endpoint, error) {
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

func stopServer(t *testing.T, server rpc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestProfileClient(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	server, endpoint, err := startServer(t, ctx)
	if err != nil {
		return
	}
	defer stopServer(t, server)
	// Setup the command-line.
	var stdout, stderr bytes.Buffer
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	exists := naming.JoinAddressName(endpoint.String(), "exists")

	// Test the 'label' command.
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"label", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := spec.Label, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'description' command.
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"description", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := spec.Description, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'spec' command.
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"specification", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := fmt.Sprintf("%#v", spec), strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'put' command.
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"put", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Profile added successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'remove' command.
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"remove", exists}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Profile removed successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
