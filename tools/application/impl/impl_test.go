package impl_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	iapp "veyron/services/mgmt/application"
	"veyron/tools/application/impl"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mgmt/application"
	"veyron2/vlog"
)

var (
	envelope = application.Envelope{
		Args:   []string{"arg1", "arg2", "arg3"},
		Binary: "/path/to/binary",
		Env:    []string{"env1", "env2", "env3"},
	}
	jsonEnv = `{
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
  ]
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

type dispatcher struct {
}

func NewDispatcher() *dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(iapp.NewServerRepository(&server{suffix: suffix}))
	return invoker, nil, nil
}

func startServer(t *testing.T, r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher()
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

func TestApplicationClient(t *testing.T) {
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
	appName := naming.JoinAddressName(endpoint.String(), "//myapp/1")
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
	if expected, got := "Application updated successfully.", strings.TrimSpace(stdout.String()); got != expected {
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
