// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"v.io/v23/naming"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/test"

	cmd_device "v.io/x/ref/services/device/device"
)

func TestDebugCommand(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	tapes := newTapeMap()
	server, endpoint, err := startServer(t, ctx, tapes)
	if err != nil {
		return
	}
	defer stopServer(t, server)
	// Setup the command-line.
	cmd := cmd_device.CmdRoot
	var stdout, stderr bytes.Buffer
	env := &cmdline.Env{Stdout: &stdout, Stderr: &stderr}
	appName := naming.JoinAddressName(endpoint.String(), "")

	debugMessage := "the secrets of the universe, revealed"
	rootTape := tapes.forSuffix("")
	rootTape.SetResponses(debugMessage)
	if err := v23cmd.ParseAndRunForTest(cmd, ctx, env, []string{"debug", appName}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := debugMessage, strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from debug. Got %q, expected %q", got, expected)
	}
	if got, expected := rootTape.Play(), []interface{}{"Debug"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
}
