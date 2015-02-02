package impl_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/device"
	verror "v.io/core/veyron2/verror2"

	"v.io/core/veyron/tools/mgmt/device/impl"
)

func TestListCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()

	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	deviceName := naming.JoinAddressName(endpoint.String(), "")

	// Test the 'list' command.
	tape.SetResponses([]interface{}{ListAssociationResponse{
		na: []device.Association{
			{
				"root/self",
				"alice_self_account",
			},
			{
				"root/other",
				"alice_other_account",
			},
		},
		err: nil,
	}})

	if err := cmd.Execute([]string{"associate", "list", deviceName}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "root/self alice_self_account\nroot/other alice_other_account", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	if got, expected := tape.Play(), []interface{}{"ListAssociations"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	// Test list with bad parameters.
	if err := cmd.Execute([]string{"associate", "list", deviceName, "hello"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestAddCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()

	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	deviceName := naming.JoinAddressName(endpoint.String(), "/myapp/1")

	if err := cmd.Execute([]string{"add", "one"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	tape.SetResponses([]interface{}{nil})
	if err := cmd.Execute([]string{"associate", "add", deviceName, "alice", "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected := []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/self"}, "alice"},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	tape.SetResponses([]interface{}{nil})
	if err := cmd.Execute([]string{"associate", "add", deviceName, "alice", "root/other", "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected = []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/other", "root/self"}, "alice"},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestRemoveCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()

	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	deviceName := naming.JoinAddressName(endpoint.String(), "")

	if err := cmd.Execute([]string{"remove", "one"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	tape.SetResponses([]interface{}{nil})
	if err := cmd.Execute([]string{"associate", "remove", deviceName, "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected := []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/self"}, ""},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestInstallCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()

	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	deviceName := naming.JoinAddressName(endpoint.String(), "")
	appId := "myBestAppID"
	cfg := device.Config{"someflag": "somevalue"}
	for i, c := range []struct {
		args         []string
		config       device.Config
		shouldErr    bool
		tapeResponse interface{}
		expectedTape interface{}
	}{
		{
			[]string{"install", "blech"},
			nil,
			true,
			nil,
			nil,
		},
		{
			[]string{"install", "blech1", "blech2", "blech3", "blech4"},
			nil,
			true,
			nil,
			nil,
		},
		{
			[]string{"install", deviceName, appNameNoFetch, "not-valid-json"},
			nil,
			true,
			nil,
			nil,
		},
		{
			[]string{"install", deviceName, appNameNoFetch},
			nil,
			false,
			InstallResponse{appId, nil},
			InstallStimulus{"Install", appNameNoFetch, nil, application.Envelope{}, 0},
		},
		{
			[]string{"install", deviceName, appNameNoFetch},
			cfg,
			false,
			InstallResponse{appId, nil},
			InstallStimulus{"Install", appNameNoFetch, cfg, application.Envelope{}, 0},
		},
	} {
		tape.SetResponses([]interface{}{c.tapeResponse})
		if c.config != nil {
			jsonConfig, err := json.Marshal(c.config)
			if err != nil {
				t.Fatalf("test case %d: Marshal(%v) failed: %v", i, c.config, err)
			}
			c.args = append(c.args, string(jsonConfig))
		}
		err := cmd.Execute(c.args)
		if c.shouldErr {
			if err == nil {
				t.Fatalf("test case %d: wrongly failed to receive a non-nil error.", i)
			}
			if got, expected := len(tape.Play()), 0; got != expected {
				t.Errorf("test case %d: invalid call sequence. Got %v, want %v", got, expected)
			}
		} else {
			if err != nil {
				t.Fatalf("test case %d: %v", i, err)
			}
			if expected, got := fmt.Sprintf("Successfully installed: %q", naming.Join(deviceName, appId)), strings.TrimSpace(stdout.String()); got != expected {
				t.Fatalf("test case %d: Unexpected output from Install. Got %q, expected %q", i, got, expected)
			}
			if got, expected := tape.Play(), []interface{}{c.expectedTape}; !reflect.DeepEqual(expected, got) {
				t.Errorf("test case %d: invalid call sequence. Got %v, want %v", i, got, expected)
			}
		}
		tape.Rewind()
		stdout.Reset()
	}
}

func TestClaimCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()

	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	deviceName := naming.JoinAddressName(endpoint.String(), "")

	// Confirm that we correctly enforce the number of arguments.
	if err := cmd.Execute([]string{"claim", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: claim: incorrect number of arguments, expected 2, got 1", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from claim. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	if err := cmd.Execute([]string{"claim", "nope", "nope", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: claim: incorrect number of arguments, expected 2, got 3", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from claim. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	// Correct operation.
	tape.SetResponses([]interface{}{
		nil,
	})
	if err := cmd.Execute([]string{"claim", deviceName, "grant"}); err != nil {
		t.Fatalf("Claim(%s, %s) failed: %v", deviceName, "grant", err)
	}
	if got, expected := len(tape.Play()), 1; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	if expected, got := "Successfully claimed.", strings.TrimSpace(stdout.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from claim. Got %q, expected prefix %q", got, expected)
	}
	expected := []interface{}{
		"Claim",
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// Error operation.
	tape.SetResponses([]interface{}{
		verror.Make(errOops, nil),
	})
	if err := cmd.Execute([]string{"claim", deviceName, "grant"}); err == nil {
		t.Fatalf("claim() failed to detect error", err)
	}
	expected = []interface{}{
		"Claim",
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()

}

func TestStartCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()

	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	appName := naming.JoinAddressName(endpoint.String(), "")

	// Confirm that we correctly enforce the number of arguments.
	if err := cmd.Execute([]string{"start", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: start: incorrect number of arguments, expected 2, got 1", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from start. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	if err := cmd.Execute([]string{"start", "nope", "nope", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: start: incorrect number of arguments, expected 2, got 3", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from start. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	// Correct operation.
	tape.SetResponses([]interface{}{StartResponse{
		appIds: []string{"app1", "app2"},
		err:    nil,
	},
	})
	if err := cmd.Execute([]string{"start", appName, "grant"}); err != nil {
		t.Fatalf("Start(%s, %s) failed: %v", appName, "grant", err)
	}

	b := new(bytes.Buffer)
	fmt.Fprintf(b, "Successfully started: %q\nSuccessfully started: %q", appName+"/app1", appName+"/app2")
	if expected, got := b.String(), strings.TrimSpace(stdout.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from start. Got %q, expected prefix %q", got, expected)
	}
	expected := []interface{}{
		"Start",
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// Error operation.
	tape.SetResponses([]interface{}{StartResponse{
		[]string{},
		verror.Make(errOops, nil),
	},
	})
	if err := cmd.Execute([]string{"start", appName, "grant"}); err == nil {
		t.Fatalf("start failed to detect error")
	}
	expected = []interface{}{
		"Start",
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()
}

func TestDebugCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()
	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)
	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	appName := naming.JoinAddressName(endpoint.String(), "")

	debugMessage := "the secrets of the universe, revealed"
	tape.SetResponses([]interface{}{debugMessage})
	if err := cmd.Execute([]string{"debug", appName}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := debugMessage, strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from debug. Got %q, expected %q", got, expected)
	}
	if got, expected := tape.Play(), []interface{}{"Debug"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
}
