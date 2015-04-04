// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/application"
	"v.io/x/lib/vlog"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/services/repository"
	"v.io/x/ref/test"
)

var (
	envelope = application.Envelope{
		Title:  "fifa world cup",
		Args:   []string{"arg1", "arg2", "arg3"},
		Binary: application.SignedFile{File: "/path/to/binary"},
		Env:    []string{"env1", "env2", "env3"},
		Packages: map[string]application.SignedFile{
			"pkg1": application.SignedFile{
				File: "/path/to/package1",
			},
		},
	}
	jsonEnv = `{
  "Title": "fifa world cup",
  "Args": [
    "arg1",
    "arg2",
    "arg3"
  ],
  "Binary": {
    "File": "/path/to/binary",
    "Signature": {
      "Purpose": null,
      "Hash": "",
      "R": null,
      "S": null
    }
  },
  "Publisher": "",
  "Env": [
    "env1",
    "env2",
    "env3"
  ],
  "Packages": {
    "pkg1": {
      "File": "/path/to/package1",
      "Signature": {
        "Purpose": null,
        "Hash": "",
        "R": null,
        "S": null
      }
    }
  }
}`
)

//go:generate v23 test generate

type server struct {
	suffix string
}

func (s *server) Match(_ rpc.ServerCall, profiles []string) (application.Envelope, error) {
	vlog.VI(2).Infof("%v.Match(%v) was called", s.suffix, profiles)
	return envelope, nil
}

func (s *server) Put(_ rpc.ServerCall, profiles []string, env application.Envelope) error {
	vlog.VI(2).Infof("%v.Put(%v, %v) was called", s.suffix, profiles, env)
	return nil
}

func (s *server) Remove(_ rpc.ServerCall, profile string) error {
	vlog.VI(2).Infof("%v.Remove(%v) was called", s.suffix, profile)
	return nil
}

func (s *server) SetPermissions(_ rpc.ServerCall, acl access.Permissions, etag string) error {
	vlog.VI(2).Infof("%v.SetPermissions(%v, %v) was called", acl, etag)
	return nil
}

func (s *server) GetPermissions(rpc.ServerCall) (access.Permissions, string, error) {
	vlog.VI(2).Infof("%v.GetPermissions() was called")
	return nil, "", nil
}

type dispatcher struct {
}

func NewDispatcher() rpc.Dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.ApplicationServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (rpc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher()
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
		t.Errorf("Serve failed: %v", err)
		return nil, nil, err
	}
	return server, endpoints[0], nil
}

func stopServer(t *testing.T, server rpc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestApplicationClient(t *testing.T) {
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
	appName := naming.JoinAddressName(endpoint.String(), "myapp/1")
	profile := "myprofile"

	// Test the 'Match' command.
	if err := cmd.Execute([]string{"match", appName, profile}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := jsonEnv, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from match. Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'put' command.
	f, err := ioutil.TempFile("", "test")
	if err != nil {
		t.Fatalf("%v", err)
	}
	fileName := f.Name()
	defer os.Remove(fileName)
	if _, err = f.Write([]byte(jsonEnv)); err != nil {
		t.Fatalf("%v", err)
	}
	if err = f.Close(); err != nil {
		t.Fatalf("%v", err)
	}
	if err := cmd.Execute([]string{"put", appName, profile, fileName}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Application envelope added successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from put. Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'remove' command.
	if err := cmd.Execute([]string{"remove", appName, profile}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Application envelope removed successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from remove. Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'edit' command. (nothing changed)
	os.Setenv("EDITOR", "true")
	if err := cmd.Execute([]string{"edit", appName, profile}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Nothing changed", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from edit. Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'edit' command.
	os.Setenv("EDITOR", "perl -pi -e 's/arg1/arg111/'")
	if err := cmd.Execute([]string{"edit", appName, profile}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Application envelope updated successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from edit. Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
