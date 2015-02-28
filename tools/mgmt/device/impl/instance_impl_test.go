package impl_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"v.io/v23/naming"
	"v.io/v23/verror"

	"v.io/x/ref/tools/mgmt/device/impl"
)

func TestStopCommand(t *testing.T) {
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
	if err := cmd.Execute([]string{"stop"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: stop: incorrect number of arguments, expected 1, got 0", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from stop. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	if err := cmd.Execute([]string{"stop", "nope", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: stop: incorrect number of arguments, expected 1, got 2", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from stop. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	// Test the 'stop' command.
	tape.SetResponses([]interface{}{
		nil,
	})

	if err := cmd.Execute([]string{"stop", appName + "/appname"}); err != nil {
		t.Fatalf("stop failed when it shouldn't: %v", err)
	}
	if expected, got := "Stop succeeded", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	expected := []interface{}{
		StopStimulus{"Stop", 5},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stderr.Reset()
	stdout.Reset()

	// Test stop with bad parameters.
	tape.SetResponses([]interface{}{
		verror.New(errOops, nil),
	})
	if err := cmd.Execute([]string{"stop", appName + "/appname"}); err == nil {
		t.Fatalf("wrongly didn't receive a non-nil error.")
	}
	// expected the same.
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stderr.Reset()
	stdout.Reset()
}

func testHelper(t *testing.T, lower, upper string) {
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
	if err := cmd.Execute([]string{lower}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: "+lower+": incorrect number of arguments, expected 1, got 0", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from %s. Got %q, expected prefix %q", lower, got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	if err := cmd.Execute([]string{lower, "nope", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: "+lower+": incorrect number of arguments, expected 1, got 2", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from %s. Got %q, expected prefix %q", lower, got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	// Correct operation.
	tape.SetResponses([]interface{}{
		nil,
	})
	if err := cmd.Execute([]string{lower, appName + "/appname"}); err != nil {
		t.Fatalf("%s failed when it shouldn't: %v", lower, err)
	}
	if expected, got := upper+" succeeded", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from %s. Got %q, expected %q", lower, got, expected)
	}
	if expected, got := []interface{}{upper}, tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stderr.Reset()
	stdout.Reset()

	// Test list with bad parameters.
	tape.SetResponses([]interface{}{
		verror.New(errOops, nil),
	})
	if err := cmd.Execute([]string{lower, appName + "/appname"}); err == nil {
		t.Fatalf("wrongly didn't receive a non-nil error.")
	}
	// expected the same.
	if expected, got := []interface{}{upper}, tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stderr.Reset()
	stdout.Reset()
}

func TestSuspendCommand(t *testing.T) {
	testHelper(t, "suspend", "Suspend")
}

func TestResumeCommand(t *testing.T) {
	testHelper(t, "resume", "Resume")
}
