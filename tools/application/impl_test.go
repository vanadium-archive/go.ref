package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/vlog"

	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/profiles"
	"v.io/core/veyron/services/mgmt/repository"
)

var (
	envelope = application.Envelope{
		Title:  "fifa world cup",
		Args:   []string{"arg1", "arg2", "arg3"},
		Binary: "/path/to/binary",
		Env:    []string{"env1", "env2", "env3"},
		Packages: map[string]string{
			"pkg1": "/path/to/package1",
		},
	}
	jsonEnv = `{
  "Title": "fifa world cup",
  "Args": [
    "arg1",
    "arg2",
    "arg3"
  ],
  "Binary": "/path/to/binary",
  "Env": [
    "env1",
    "env2",
    "env3"
  ],
  "Packages": {
    "pkg1": "/path/to/package1"
  }
}`
)

type server struct {
	suffix string
}

func (s *server) Match(_ ipc.ServerContext, profiles []string) (application.Envelope, error) {
	vlog.VI(2).Infof("%v.Match(%v) was called", s.suffix, profiles)
	return envelope, nil
}

func (s *server) Put(_ ipc.ServerContext, profiles []string, env application.Envelope) error {
	vlog.VI(2).Infof("%v.Put(%v, %v) was called", s.suffix, profiles, env)
	return nil
}

func (s *server) Remove(_ ipc.ServerContext, profile string) error {
	vlog.VI(2).Infof("%v.Remove(%v) was called", s.suffix, profile)
	return nil
}

func (s *server) SetACL(_ ipc.ServerContext, acl access.TaggedACLMap, etag string) error {
	vlog.VI(2).Infof("%v.SetACL(%v, %v) was called", acl, etag)
	return nil
}

func (s *server) GetACL(ipc.ServerContext) (access.TaggedACLMap, string, error) {
	vlog.VI(2).Infof("%v.GetACL() was called")
	return nil, "", nil
}

type dispatcher struct {
}

func NewDispatcher() *dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.ApplicationServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (ipc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher()
	server, err := veyron2.NewServer(ctx)
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
		t.Errorf("Serve failed: %v", err)
		return nil, nil, err
	}
	return server, endpoints[0], nil
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestApplicationClient(t *testing.T) {
	var err error
	// TODO(ataly, mattr, suharshs): This is a HACK to ensure that the server and the
	// client have the same freshly created principal. One way to avoid the RuntimePrincipal
	// option is to have a global client context.T (in main.go) instead of a veyron2.Runtime.
	runtime, err = rt.New(options.RuntimePrincipal{tsecurity.NewPrincipal("test-blessing")})
	if err != nil {
		t.Fatalf("Unexpected error initializing runtime: %s", err)
	}
	defer runtime.Cleanup()
	ctx := runtime.NewContext()

	server, endpoint, err := startServer(t, ctx)
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
