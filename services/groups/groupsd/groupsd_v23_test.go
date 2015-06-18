// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"

	"v.io/v23/naming"
	"v.io/v23/services/groups"
	"v.io/x/lib/set"
	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate

type relateResult struct {
	Remainder      map[string]struct{}
	Approximations []groups.Approximation
	Version        string
}

// V23TestGroupServerIntegration tests the integration between the
// "groups" command-line client and the "groupsd" server.
func V23TestGroupServerIntegration(t *v23tests.T) {
	v23tests.RunRootMT(t, "--v23.tcp.address=127.0.0.1:0")

	// Build binaries for the client and server. Since permissions are
	// not setup on the server, the client must pass the default
	// authorization policy, i.e., must be a "delegate" of the server.
	var (
		clientBin  = binaryWithCredentials(t, "groupsd/client", "v.io/x/ref/services/groups/groups")
		serverBin  = binaryWithCredentials(t, "groupsd", "v.io/x/ref/services/groups/groupsd")
		serverName = "test-groups-server"
		groupA     = naming.Join(serverName, "groupA")
		groupB     = naming.Join(serverName, "groupB")
	)

	// Start the groups server.
	serverBin.Start("-name="+serverName, "-v23.tcp.address=127.0.0.1:0")

	// Create a couple of groups.
	clientBin.Start("create", groupA).WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("create", groupB, "a", "a/b").WaitOrDie(os.Stdout, os.Stderr)

	// Add a couple of blessing patterns.
	clientBin.Start("add", groupA, "<grp:test-groups-server/groupB>").WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("add", groupA, "a").WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("add", groupB, "a/b/c").WaitOrDie(os.Stdout, os.Stderr)

	// Remove a blessing pattern.
	clientBin.Start("remove", groupB, "a").WaitOrDie(os.Stdout, os.Stderr)

	// Test simple group resolution.
	{
		var buffer bytes.Buffer
		clientBin.Start("relate", groupB, "a/b/c/d").WaitOrDie(&buffer, &buffer)
		var got relateResult
		if err := json.Unmarshal(buffer.Bytes(), &got); err != nil {
			t.Fatalf("Unmarshal(%v) failed: %v", buffer.Bytes(), err)
		}
		want := relateResult{
			Remainder:      set.String.FromSlice([]string{"c/d", "d"}),
			Approximations: nil,
			Version:        "2",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	}

	// Test recursive group resolution.
	{
		var buffer bytes.Buffer
		clientBin.Start("relate", groupA, "a/b/c/d").WaitOrDie(&buffer, &buffer)
		var got relateResult
		if err := json.Unmarshal(buffer.Bytes(), &got); err != nil {
			t.Fatalf("Unmarshal(%v) failed: %v", buffer.Bytes(), err)
		}
		want := relateResult{
			Remainder:      set.String.FromSlice([]string{"b/c/d", "c/d", "d"}),
			Approximations: nil,
			Version:        "2",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	}

	// Test group resolution failure. Note that under-approximation is
	// used as the default to handle resolution failures.
	{
		clientBin.Start("add", groupB, "<grp:test-groups-server/groupC>").WaitOrDie(os.Stdout, os.Stderr)
		var buffer bytes.Buffer
		clientBin.Start("relate", groupB, "a/b/c/d").WaitOrDie(&buffer, &buffer)
		var got relateResult
		if err := json.Unmarshal(buffer.Bytes(), &got); err != nil {
			t.Fatalf("Unmarshal(%v) failed: %v", buffer.Bytes(), err)
		}
		want := relateResult{
			Remainder: set.String.FromSlice([]string{"c/d", "d"}),
			Approximations: []groups.Approximation{
				groups.Approximation{
					Reason:  "v.io/v23/verror.NoExist",
					Details: `groupsd:"groupC".Relate: Does not exist: groupC`,
				},
			},
			Version: "3",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	}

	// Delete the groups.
	clientBin.Start("delete", groupA).WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("delete", groupB).WaitOrDie(os.Stdout, os.Stderr)
}

func binaryWithCredentials(t *v23tests.T, extension, pkgpath string) *v23tests.Binary {
	creds, err := t.Shell().NewChildCredentials(extension)
	if err != nil {
		t.Fatalf("NewChildsCredentials(%v) failed: %v", extension, err)
	}
	b := t.BuildV23Pkg(pkgpath)
	return b.WithStartOpts(b.StartOpts().WithCustomCredentials(creds))
}
