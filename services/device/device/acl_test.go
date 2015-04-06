// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/verror"

	cmd_device "v.io/x/ref/services/device/device"
)

const pkgPath = "v.io/x/ref/services/device/main"

var (
	errOops = verror.Register(pkgPath+".errOops", verror.NoRetry, "oops!")
)

func TestAccessListGetCommand(t *testing.T) {
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
	deviceName := endpoint.Name()

	// Test the 'get' command.
	tape.SetResponses([]interface{}{GetPermissionsResponse{
		acl: access.Permissions{
			"Admin": access.AccessList{
				In:    []security.BlessingPattern{"self"},
				NotIn: []string{"self/bad"},
			},
			"Read": access.AccessList{
				In: []security.BlessingPattern{"other", "self"},
			},
		},
		etag: "anEtagForToday",
		err:  nil,
	}})

	if err := cmd.Execute([]string{"acl", "get", deviceName}); err != nil {
		t.Fatalf("%v, output: %v, error: %v", err)
	}
	if expected, got := strings.TrimSpace(`
other Read
self Admin,Read
self/bad !Admin
`), strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Unexpected output from get. Got %q, expected %q", got, expected)
	}
	if got, expected := tape.Play(), []interface{}{"GetPermissions"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %#v, want %#v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestAccessListSetCommand(t *testing.T) {
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
	deviceName := endpoint.Name()

	// Some tests to validate parse.
	if err := cmd.Execute([]string{"acl", "set", deviceName}); err == nil {
		t.Fatalf("failed to correctly detect insufficient parameters")
	}
	if expected, got := "ERROR: set: incorrect number of arguments 1, must be 1 + 2n", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, expected prefix %q", got, expected)
	}

	stderr.Reset()
	stdout.Reset()
	if err := cmd.Execute([]string{"acl", "set", deviceName, "foo"}); err == nil {
		t.Fatalf("failed to correctly detect insufficient parameters")
	}
	if expected, got := "ERROR: set: incorrect number of arguments 2, must be 1 + 2n", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, expected prefix %q", got, expected)
	}

	stderr.Reset()
	stdout.Reset()
	if err := cmd.Execute([]string{"acl", "set", deviceName, "foo", "bar", "ohno"}); err == nil {
		t.Fatalf("failed to correctly detect insufficient parameters")
	}
	if expected, got := "ERROR: set: incorrect number of arguments 4, must be 1 + 2n", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, expected prefix %q", got, expected)
	}

	stderr.Reset()
	stdout.Reset()
	if err := cmd.Execute([]string{"acl", "set", deviceName, "foo", "!"}); err == nil {
		t.Fatalf("failed to detect invalid parameter")
	}
	if expected, got := "ERROR: failed to parse access tags for \"foo\": empty access tag", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Errorf("Unexpected output from list. Got %q, expected prefix %q", got, expected)
	}

	// Correct operation in the absence of errors.
	stderr.Reset()
	stdout.Reset()
	tape.SetResponses([]interface{}{GetPermissionsResponse{
		acl: access.Permissions{
			"Admin": access.AccessList{
				In: []security.BlessingPattern{"self"},
			},
			"Read": access.AccessList{
				In:    []security.BlessingPattern{"other", "self"},
				NotIn: []string{"other/bob"},
			},
		},
		etag: "anEtagForToday",
		err:  nil,
	},
		verror.NewErrBadEtag(nil),
		GetPermissionsResponse{
			acl: access.Permissions{
				"Admin": access.AccessList{
					In: []security.BlessingPattern{"self"},
				},
				"Read": access.AccessList{
					In:    []security.BlessingPattern{"other", "self"},
					NotIn: []string{"other/bob/baddevice"},
				},
			},
			etag: "anEtagForTomorrow",
			err:  nil,
		},
		nil,
	})

	// set command that:
	// - Adds entry for "friends" to "Write" & "Admin"
	// - Adds a blacklist entry for "friend/alice"  for "Admin"
	// - Edits existing entry for "self" (adding "Write" access)
	// - Removes entry for "other/bob/baddevice"
	if err := cmd.Execute([]string{
		"acl",
		"set",
		deviceName,
		"friends", "Admin,Write",
		"friends/alice", "!Admin,Write",
		"self", "Admin,Write,Read",
		"other/bob/baddevice", "^",
	}); err != nil {
		t.Fatalf("SetPermissions failed: %v", err)
	}

	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	if expected, got := "WARNING: trying again because of asynchronous change", strings.TrimSpace(stderr.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	expected := []interface{}{
		"GetPermissions",
		SetPermissionsStimulus{
			fun: "SetPermissions",
			acl: access.Permissions{
				"Admin": access.AccessList{
					In:    []security.BlessingPattern{"friends", "self"},
					NotIn: []string{"friends/alice"},
				},
				"Read": access.AccessList{
					In:    []security.BlessingPattern{"other", "self"},
					NotIn: []string{"other/bob"},
				},
				"Write": access.AccessList{
					In:    []security.BlessingPattern{"friends", "friends/alice", "self"},
					NotIn: []string(nil),
				},
			},
			etag: "anEtagForToday",
		},
		"GetPermissions",
		SetPermissionsStimulus{
			fun: "SetPermissions",
			acl: access.Permissions{
				"Admin": access.AccessList{
					In:    []security.BlessingPattern{"friends", "self"},
					NotIn: []string{"friends/alice"},
				},
				"Read": access.AccessList{
					In:    []security.BlessingPattern{"other", "self"},
					NotIn: []string(nil),
				},
				"Write": access.AccessList{
					In:    []security.BlessingPattern{"friends", "friends/alice", "self"},
					NotIn: []string(nil),
				},
			},
			etag: "anEtagForTomorrow",
		},
	}

	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %#v, want %#v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// GetPermissions fails.
	tape.SetResponses([]interface{}{GetPermissionsResponse{
		acl:  access.Permissions{},
		etag: "anEtagForToday",
		err:  verror.New(errOops, nil),
	},
	})

	if err := cmd.Execute([]string{"acl", "set", deviceName, "vana/bad", "Read"}); err == nil {
		t.Fatalf("GetPermissions RPC inside acl set command failed but error wrongly not detected")
	} else if expected, got := `^GetPermissions\(`+deviceName+`\) failed:.*oops!`, err.Error(); !regexp.MustCompile(expected).MatchString(got) {
		t.Fatalf("Unexpected output from list. Got %q, regexp %q", got, expected)
	}
	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	expected = []interface{}{
		"GetPermissions",
	}

	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %#v, want %#v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// SetPermissions fails with something other than a bad etag failure.
	tape.SetResponses([]interface{}{GetPermissionsResponse{
		acl: access.Permissions{
			"Read": access.AccessList{
				In: []security.BlessingPattern{"other", "self"},
			},
		},
		etag: "anEtagForToday",
		err:  nil,
	},
		verror.New(errOops, nil),
	})

	if err := cmd.Execute([]string{"acl", "set", deviceName, "friend", "Read"}); err == nil {
		t.Fatalf("SetPermissions should have failed: %v", err)
	} else if expected, got := `^SetPermissions\(`+deviceName+`\) failed:.*oops!`, err.Error(); !regexp.MustCompile(expected).MatchString(got) {
		t.Fatalf("Unexpected output from list. Got %q, regexp %q", got, expected)
	}
	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}

	expected = []interface{}{
		"GetPermissions",
		SetPermissionsStimulus{
			fun: "SetPermissions",
			acl: access.Permissions{
				"Read": access.AccessList{
					In:    []security.BlessingPattern{"friend", "other", "self"},
					NotIn: []string(nil),
				},
			},
			etag: "anEtagForToday",
		},
	}

	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %#v, want %#v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()
}
