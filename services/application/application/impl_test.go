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

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/application"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/lib/xrpc"
	_ "v.io/x/ref/runtime/factories/generic"
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
		Restarts:          0,
		RestartTimeWindow: 0,
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
  },
  "Restarts": 0,
  "RestartTimeWindow": 0
}`
)

//go:generate v23 test generate

type server struct {
	suffix string
}

func (s *server) Match(ctx *context.T, _ rpc.ServerCall, profiles []string) (application.Envelope, error) {
	ctx.VI(2).Infof("%v.Match(%v) was called", s.suffix, profiles)
	return envelope, nil
}

func (s *server) Put(ctx *context.T, _ rpc.ServerCall, profiles []string, env application.Envelope) error {
	ctx.VI(2).Infof("%v.Put(%v, %v) was called", s.suffix, profiles, env)
	return nil
}

func (s *server) Remove(ctx *context.T, _ rpc.ServerCall, profile string) error {
	ctx.VI(2).Infof("%v.Remove(%v) was called", s.suffix, profile)
	return nil
}

func (s *server) SetPermissions(ctx *context.T, _ rpc.ServerCall, perms access.Permissions, version string) error {
	ctx.VI(2).Infof("%v.SetPermissions(%v, %v) was called", perms, version)
	return nil
}

func (s *server) GetPermissions(ctx *context.T, _ rpc.ServerCall) (access.Permissions, string, error) {
	ctx.VI(2).Infof("%v.GetPermissions() was called")
	return nil, "", nil
}

func (s *server) TidyNow(ctx *context.T, _ rpc.ServerCall) error {
	ctx.VI(2).Infof("%v.TidyNow() was called", s)
	return nil
}

type dispatcher struct{}

func (d *dispatcher) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
	return repository.ApplicationServer(&server{suffix: suffix}), nil, nil
}

func TestApplicationClient(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	server, err := xrpc.NewDispatchingServer(ctx, "", &dispatcher{})
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return
	}
	endpoint := server.Status().Endpoints[0]

	// Setup the command-line.
	var stdout, stderr bytes.Buffer
	env := &cmdline.Env{Stdout: &stdout, Stderr: &stderr}
	appName := naming.JoinAddressName(endpoint.String(), "myapp/1")
	profile := "myprofile"

	// Test the 'Match' command.
	if err := v23cmd.ParseAndRunForTest(cmdRoot, ctx, env, []string{"match", appName, profile}); err != nil {
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
	if err := v23cmd.ParseAndRunForTest(cmdRoot, ctx, env, []string{"put", appName, profile, fileName}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Application envelope added successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from put. Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'remove' command.
	if err := v23cmd.ParseAndRunForTest(cmdRoot, ctx, env, []string{"remove", appName, profile}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Application envelope removed successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from remove. Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'edit' command. (nothing changed)
	env.Vars = map[string]string{"EDITOR": "true"}
	if err := v23cmd.ParseAndRunForTest(cmdRoot, ctx, env, []string{"edit", appName, profile}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Nothing changed", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from edit. Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'edit' command.
	env.Vars = map[string]string{"EDITOR": "perl -pi -e 's/arg1/arg111/'"}
	if err := v23cmd.ParseAndRunForTest(cmdRoot, ctx, env, []string{"edit", appName, profile}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Application envelope updated successfully.", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from edit. Got %q, expected %q", got, expected)
	}
	stdout.Reset()
}
