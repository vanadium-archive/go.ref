// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/services/application"
	"v.io/v23/services/device"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline2"
	"v.io/x/ref/lib/security"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/test"

	cmd_device "v.io/x/ref/services/device/device"
)

//go:generate v23 test generate

func TestListCommand(t *testing.T) {
	ctx, shutdown := test.InitForTest()
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
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	deviceName := naming.JoinAddressName(endpoint.String(), "")

	rootTape := tapes.forSuffix("")
	// Test the 'list' command.
	rootTape.SetResponses([]interface{}{ListAssociationResponse{
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

	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"associate", "list", deviceName}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "root/self alice_self_account\nroot/other alice_other_account", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	if got, expected := rootTape.Play(), []interface{}{"ListAssociations"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	rootTape.Rewind()
	stdout.Reset()

	// Test list with bad parameters.
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"associate", "list", deviceName, "hello"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(rootTape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
}

func TestAddCommand(t *testing.T) {
	ctx, shutdown := test.InitForTest()
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
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	deviceName := naming.JoinAddressName(endpoint.String(), "")

	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"add", "one"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	rootTape := tapes.forSuffix("")
	if got, expected := len(rootTape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	rootTape.Rewind()
	stdout.Reset()

	rootTape.SetResponses([]interface{}{nil})
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"associate", "add", deviceName, "alice", "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected := []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/self"}, "alice"},
	}
	if got := rootTape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	rootTape.Rewind()
	stdout.Reset()

	rootTape.SetResponses([]interface{}{nil})
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"associate", "add", deviceName, "alice", "root/other", "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected = []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/other", "root/self"}, "alice"},
	}
	if got := rootTape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
}

func TestRemoveCommand(t *testing.T) {
	ctx, shutdown := test.InitForTest()
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
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	deviceName := naming.JoinAddressName(endpoint.String(), "")

	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"remove", "one"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	rootTape := tapes.forSuffix("")
	if got, expected := len(rootTape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	rootTape.Rewind()
	stdout.Reset()

	rootTape.SetResponses([]interface{}{nil})
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"associate", "remove", deviceName, "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected := []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/self"}, ""},
	}
	if got := rootTape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
}

func TestInstallCommand(t *testing.T) {
	ctx, shutdown := test.InitForTest()
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
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	deviceName := naming.JoinAddressName(endpoint.String(), "")
	appId := "myBestAppID"
	cfg := device.Config{"someflag": "somevalue"}
	pkg := application.Packages{"pkg": application.SignedFile{File: "somename"}}
	rootTape := tapes.forSuffix("")
	for i, c := range []struct {
		args         []string
		config       device.Config
		packages     application.Packages
		shouldErr    bool
		tapeResponse interface{}
		expectedTape interface{}
	}{
		{
			[]string{"blech"},
			nil,
			nil,
			true,
			nil,
			nil,
		},
		{
			[]string{"blech1", "blech2", "blech3", "blech4"},
			nil,
			nil,
			true,
			nil,
			nil,
		},
		{
			[]string{deviceName, appNameNoFetch, "not-valid-json"},
			nil,
			nil,
			true,
			nil,
			nil,
		},
		{
			[]string{deviceName, appNameNoFetch},
			nil,
			nil,
			false,
			InstallResponse{appId, nil},
			InstallStimulus{"Install", appNameNoFetch, nil, nil, application.Envelope{}, nil},
		},
		{
			[]string{deviceName, appNameNoFetch},
			cfg,
			pkg,
			false,
			InstallResponse{appId, nil},
			InstallStimulus{"Install", appNameNoFetch, cfg, pkg, application.Envelope{}, nil},
		},
	} {
		rootTape.SetResponses([]interface{}{c.tapeResponse})
		if c.config != nil {
			jsonConfig, err := json.Marshal(c.config)
			if err != nil {
				t.Fatalf("test case %d: Marshal(%v) failed: %v", i, c.config, err)
			}
			c.args = append([]string{fmt.Sprintf("--config=%s", string(jsonConfig))}, c.args...)
		}
		if c.packages != nil {
			jsonPackages, err := json.Marshal(c.packages)
			if err != nil {
				t.Fatalf("test case %d: Marshal(%v) failed: %v", i, c.packages, err)
			}
			c.args = append([]string{fmt.Sprintf("--packages=%s", string(jsonPackages))}, c.args...)
		}
		c.args = append([]string{"install"}, c.args...)
		err := v23cmd.ParseAndRun(cmd, ctx, env, c.args)
		if c.shouldErr {
			if err == nil {
				t.Fatalf("test case %d: wrongly failed to receive a non-nil error.", i)
			}
			if got, expected := len(rootTape.Play()), 0; got != expected {
				t.Errorf("test case %d: invalid call sequence. Got %v, want %v", got, expected)
			}
		} else {
			if err != nil {
				t.Fatalf("test case %d: %v", i, err)
			}
			if expected, got := naming.Join(deviceName, appId), strings.TrimSpace(stdout.String()); got != expected {
				t.Fatalf("test case %d: Unexpected output from Install. Got %q, expected %q", i, got, expected)
			}
			if got, expected := rootTape.Play(), []interface{}{c.expectedTape}; !reflect.DeepEqual(expected, got) {
				t.Errorf("test case %d: invalid call sequence. Got %#v, want %#v", i, got, expected)
			}
		}
		rootTape.Rewind()
		stdout.Reset()
	}
}

func TestClaimCommand(t *testing.T) {
	ctx, shutdown := test.InitForTest()
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
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	deviceName := naming.JoinAddressName(endpoint.String(), "")
	deviceKey, err := v23.GetPrincipal(ctx).PublicKey().MarshalBinary()
	if err != nil {
		t.Fatalf("Failed to marshal principal public key: %v", err)
	}

	// Confirm that we correctly enforce the number of arguments.
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"claim", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: claim: incorrect number of arguments, expected atleast 2 (max: 4), got 1", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from claim. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	rootTape := tapes.forSuffix("")
	rootTape.Rewind()

	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"claim", "nope", "nope", "nope", "nope", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: claim: incorrect number of arguments, expected atleast 2 (max: 4), got 5", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from claim. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	rootTape.Rewind()

	// Incorrect operation
	var pairingToken string
	var deviceKeyWrong []byte
	if publicKey, _, err := security.NewPrincipalKey(); err != nil {
		t.Fatalf("NewPrincipalKey failed: %v", err)
	} else {
		if deviceKeyWrong, err = publicKey.MarshalBinary(); err != nil {
			t.Fatalf("Failed to marshal principal public key: %v", err)
		}
	}
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"claim", deviceName, "grant", pairingToken, base64.URLEncoding.EncodeToString(deviceKeyWrong)}); verror.ErrorID(err) != verror.ErrNotTrusted.ID {
		t.Fatalf("wrongly failed to receive correct error on claim with incorrect device key:%v id:%v", err, verror.ErrorID(err))
	}
	stdout.Reset()
	stderr.Reset()
	rootTape.Rewind()

	// Correct operation.
	rootTape.SetResponses([]interface{}{
		nil,
	})
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"claim", deviceName, "grant", pairingToken, base64.URLEncoding.EncodeToString(deviceKey)}); err != nil {
		t.Fatalf("Claim(%s, %s, %s) failed: %v", deviceName, "grant", pairingToken, err)
	}
	if got, expected := len(rootTape.Play()), 1; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	if expected, got := "Successfully claimed.", strings.TrimSpace(stdout.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from claim. Got %q, expected prefix %q", got, expected)
	}
	expected := []interface{}{
		"Claim",
	}
	if got := rootTape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	rootTape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// Error operation.
	rootTape.SetResponses([]interface{}{
		verror.New(errOops, nil),
	})
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"claim", deviceName, "grant", pairingToken}); err == nil {
		t.Fatalf("claim() failed to detect error", err)
	}
	expected = []interface{}{
		"Claim",
	}
	if got := rootTape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
}

func TestInstantiateCommand(t *testing.T) {
	ctx, shutdown := test.InitForTest()
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
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	appName := naming.JoinAddressName(endpoint.String(), "")

	// Confirm that we correctly enforce the number of arguments.
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"instantiate", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: instantiate: incorrect number of arguments, expected 2, got 1", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from instantiate. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	rootTape := tapes.forSuffix("")
	rootTape.Rewind()

	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"instantiate", "nope", "nope", "nope"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if expected, got := "ERROR: instantiate: incorrect number of arguments, expected 2, got 3", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from instantiate. Got %q, expected prefix %q", got, expected)
	}
	stdout.Reset()
	stderr.Reset()
	rootTape.Rewind()

	// Correct operation.
	rootTape.SetResponses([]interface{}{InstantiateResponse{
		err:        nil,
		instanceID: "app1",
	},
	})
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"instantiate", appName, "grant"}); err != nil {
		t.Fatalf("instantiate %s %s failed: %v", appName, "grant", err)
	}

	b := new(bytes.Buffer)
	fmt.Fprintf(b, "%s", appName+"/app1")
	if expected, got := b.String(), strings.TrimSpace(stdout.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from instantiate. Got %q, expected prefix %q", got, expected)
	}
	expected := []interface{}{
		"Instantiate",
	}
	if got := rootTape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	rootTape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// Error operation.
	rootTape.SetResponses([]interface{}{InstantiateResponse{
		verror.New(errOops, nil),
		"",
	},
	})
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"instantiate", appName, "grant"}); err == nil {
		t.Fatalf("instantiate failed to detect error")
	}
	expected = []interface{}{
		"Instantiate",
	}
	if got := rootTape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
}

func TestDebugCommand(t *testing.T) {
	ctx, shutdown := test.InitForTest()
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
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	appName := naming.JoinAddressName(endpoint.String(), "")

	debugMessage := "the secrets of the universe, revealed"
	rootTape := tapes.forSuffix("")
	rootTape.SetResponses([]interface{}{debugMessage})
	if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"debug", appName}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := debugMessage, strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from debug. Got %q, expected %q", got, expected)
	}
	if got, expected := rootTape.Play(), []interface{}{"Debug"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
}

func TestStatusCommand(t *testing.T) {
	ctx, shutdown := test.InitForTest()
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
	env := &cmdline2.Env{Stdout: &stdout, Stderr: &stderr}
	appName := naming.JoinAddressName(endpoint.String(), "")

	rootTape := tapes.forSuffix("")
	for _, c := range []struct {
		tapeResponse interface{}
		expected     string
	}{
		{
			device.StatusInstallation{device.InstallationStatus{
				State:   device.InstallationStateUninstalled,
				Version: "director's cut",
			}},
			"Installation [State:Uninstalled,Version:director's cut]",
		},
		{
			device.StatusInstance{device.InstanceStatus{
				State:   device.InstanceStateUpdating,
				Version: "theatrical version",
			}},
			"Instance [State:Updating,Version:theatrical version]",
		},
	} {
		rootTape.Rewind()
		stdout.Reset()
		rootTape.SetResponses([]interface{}{c.tapeResponse})
		if err := v23cmd.ParseAndRun(cmd, ctx, env, []string{"status", appName}); err != nil {
			t.Errorf("%v", err)
		}
		if expected, got := c.expected, strings.TrimSpace(stdout.String()); got != expected {
			t.Errorf("Unexpected output from status. Got %q, expected %q", got, expected)
		}
		if got, expected := rootTape.Play(), []interface{}{"Status"}; !reflect.DeepEqual(expected, got) {
			t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
		}
	}
}
