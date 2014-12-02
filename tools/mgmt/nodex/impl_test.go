package main

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/node"
	"veyron.io/veyron/veyron2/verror"
)

func TestListCommand(t *testing.T) {
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "")

	// Test the 'list' command.
	tape.SetResponses([]interface{}{ListAssociationResponse{
		na: []node.Association{
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

	if err := cmd.Execute([]string{"associate", "list", nodeName}); err != nil {
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
	if err := cmd.Execute([]string{"associate", "list", nodeName, "hello"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestAddCommand(t *testing.T) {
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "/myapp/1")

	if err := cmd.Execute([]string{"add", "one"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	tape.SetResponses([]interface{}{nil})
	if err := cmd.Execute([]string{"associate", "add", nodeName, "alice", "root/self"}); err != nil {
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
	if err := cmd.Execute([]string{"associate", "add", nodeName, "alice", "root/other", "root/self"}); err != nil {
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
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "")

	if err := cmd.Execute([]string{"remove", "one"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	tape.SetResponses([]interface{}{nil})
	if err := cmd.Execute([]string{"associate", "remove", nodeName, "root/self"}); err != nil {
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
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "")

	if err := cmd.Execute([]string{"install", "blech"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	if err := cmd.Execute([]string{"install", "blech1", "blech2", "blech3"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	appId := "myBestAppID"
	tape.SetResponses([]interface{}{InstallResponse{
		appId: appId,
		err:   nil,
	}})
	if err := cmd.Execute([]string{"install", nodeName, "myBestApp"}); err != nil {
		t.Fatalf("%v", err)
	}

	eb := new(bytes.Buffer)
	fmt.Fprintf(eb, "Successfully installed: %q", naming.Join(nodeName, appId))
	if expected, got := eb.String(), strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from Install. Got %q, expected %q", got, expected)
	}
	expected := []interface{}{
		InstallStimulus{"Install", "myBestApp"},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestClaimCommand(t *testing.T) {
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "")

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
	if err := cmd.Execute([]string{"claim", nodeName, "grant"}); err != nil {
		t.Fatalf("Claim(%s, %s) failed: %v", nodeName, "grant", err)
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
		verror.BadArgf("oops!"),
	})
	if err := cmd.Execute([]string{"claim", nodeName, "grant"}); err == nil {
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
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
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
		verror.BadArgf("oops!"),
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
