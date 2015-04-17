// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"v.io/v23/naming"
	"v.io/v23/verror"

	cmd_device "v.io/x/ref/services/device/device"
)

func TestKillCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()

	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := cmd_device.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	appName := naming.JoinAddressName(endpoint.String(), "")

	// Confirm that we correctly enforce the number of arguments.
	if err := cmd.Execute([]string{"kill"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: kill: incorrect number of arguments, expected 1, got 0", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from kill. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	if err := cmd.Execute([]string{"kill", "nope", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: kill: incorrect number of arguments, expected 1, got 2", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from kill. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	tape.Rewind()

	// Test the 'kill' command.
	tape.SetResponses([]interface{}{
		nil,
	})

	if err := cmd.Execute([]string{"kill", appName + "/appname"}); err != nil {
		t.Fatalf("kill failed when it shouldn't: %v", err)
	}
	if expected, got := "Kill succeeded", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	expected := []interface{}{
		KillStimulus{"Kill", 5 * time.Second},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stderr.Reset()
	stdout.Reset()

	// Test kill with bad parameters.
	tape.SetResponses([]interface{}{
		verror.New(errOops, nil),
	})
	if err := cmd.Execute([]string{"kill", appName + "/appname"}); err == nil {
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
	cmd := cmd_device.Root()
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

func TestDeleteCommand(t *testing.T) {
	testHelper(t, "delete", "Delete")
}

func TestRunCommand(t *testing.T) {
	testHelper(t, "run", "Run")
}
