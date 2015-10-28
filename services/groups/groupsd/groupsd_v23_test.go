// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"syscall"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/groups"
	"v.io/v23/verror"
	"v.io/x/lib/set"
	"v.io/x/ref/services/groups/groupsd/testdata/kvstore"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

//go:generate jiri test generate

type relateResult struct {
	Remainder      map[string]struct{}
	Approximations []groups.Approximation
	Version        string
}

// V23TestGroupServerIntegration tests the integration between the
// "groups" command-line client and the "groupsd" server.
func V23TestGroupServerIntegration(t *v23tests.T) {
	v23tests.RunRootMT(t, "--v23.tcp.address=127.0.0.1:0")

	// Build binaries for the client and server.
	var (
		clientBin  = t.BuildV23Pkg("v.io/x/ref/services/groups/groups")
		serverBin  = t.BuildV23Pkg("v.io/x/ref/services/groups/groupsd", "-tags=leveldb")
		serverName = "groups-server"
		groupA     = naming.Join(serverName, "groupA")
		groupB     = naming.Join(serverName, "groupB")
	)

	// Start the groups server.
	serverBin.Start("-name="+serverName, "-v23.tcp.address=127.0.0.1:0")

	// Create a couple of groups.
	clientBin.Start("create", groupA).WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("create", groupB, "a", "a/b").WaitOrDie(os.Stdout, os.Stderr)

	// Add a couple of blessing patterns.
	clientBin.Start("add", groupA, "<grp:groups-server/groupB>").WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("add", groupA, "a").WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("add", groupB, "a/b/c").WaitOrDie(os.Stdout, os.Stderr)

	// Remove a blessing pattern.
	clientBin.Start("remove", groupB, "a").WaitOrDie(os.Stdout, os.Stderr)

	// Test simple group resolution.
	{
		var buffer, stderrBuf bytes.Buffer
		clientBin.Start("relate", groupB, "a/b/c/d").WaitOrDie(&buffer, &stderrBuf)
		var got relateResult
		if err := json.Unmarshal(buffer.Bytes(), &got); err != nil {
			t.Fatalf("Unmarshal(%v) failed: %v", buffer.String(), err)
		}
		want := relateResult{
			Remainder:      set.String.FromSlice([]string{"c/d", "d"}),
			Approximations: nil,
			Version:        "2",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if got, want := stderrBuf.Len(), 0; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	}

	// Test recursive group resolution.
	{
		var buffer, stderrBuf bytes.Buffer
		clientBin.Start("relate", groupA, "a/b/c/d").WaitOrDie(&buffer, &stderrBuf)
		var got relateResult
		if err := json.Unmarshal(buffer.Bytes(), &got); err != nil {
			t.Fatalf("Unmarshal(%v) failed: %v", buffer.String(), err)
		}
		want := relateResult{
			Remainder:      set.String.FromSlice([]string{"b/c/d", "c/d", "d"}),
			Approximations: nil,
			Version:        "2",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if got, want := stderrBuf.Len(), 0; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	}

	// Test group resolution failure. Note that under-approximation is
	// used as the default to handle resolution failures.
	{
		clientBin.Start("add", groupB, "<grp:groups-server/groupC>").WaitOrDie(os.Stdout, os.Stderr)
		var buffer, stderrBuf bytes.Buffer
		clientBin.Start("relate", groupB, "a/b/c/d").WaitOrDie(&buffer, &stderrBuf)
		var got relateResult
		if err := json.Unmarshal(buffer.Bytes(), &got); err != nil {
			t.Fatalf("Unmarshal(%v) failed: %v", buffer.String(), err)
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
		if got, want := stderrBuf.Len(), 0; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	}

	// Delete the groups.
	clientBin.Start("delete", groupA).WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("delete", groupB).WaitOrDie(os.Stdout, os.Stderr)
}

// store implements the kvstore.Store interface.
type store map[string]string

func (s store) Get(ctx *context.T, call rpc.ServerCall, key string) (string, error) {
	return s[key], nil
}

func (s store) Set(ctx *context.T, call rpc.ServerCall, key string, value string) error {
	s[key] = value
	return nil
}

const (
	kvServerName = "key-value-store"
	getFailed    = "GET FAILED"
	getOK        = "GET OK"
	setFailed    = "SET FAILED"
	setOK        = "SET OK"
)

var runServer = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	// Use a shorter timeout to reduce the test overall runtime as the
	// permission authorizer will attempt to connect to a non-existing
	// groups server at some point in the test.
	ctx, _ = context.WithTimeout(ctx, 2*time.Second)
	authorizer, err := groups.PermissionsAuthorizer(access.Permissions{
		"Read":  access.AccessList{In: []security.BlessingPattern{"<grp:groups-server/readers>"}},
		"Write": access.AccessList{In: []security.BlessingPattern{"<grp:groups-server/writers>"}},
	}, access.TypicalTagType())
	if err != nil {
		return err
	}
	if _, _, err := v23.WithNewServer(ctx, kvServerName, kvstore.StoreServer(&store{}), authorizer); err != nil {
		return err
	}
	modules.WaitForEOF(env.Stdin)
	return nil
}, "server")

var runClient = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	if got, want := len(args), 1; got < want {
		return fmt.Errorf("unexpected number of arguments: got %v, want at least %v", got, want)
	}
	command := args[0]
	client := kvstore.StoreClient(kvServerName)
	switch command {
	case "get":
		if got, want := len(args), 2; got != want {
			return fmt.Errorf("unexpected number of arguments: got %v, want %v", got, want)
		}
		key := args[1]
		value, err := client.Get(ctx, key)
		if err != nil {
			fmt.Fprintf(env.Stdout, "%v %v\n", getFailed, verror.ErrorID(err))
		} else {
			fmt.Fprintf(env.Stdout, "%v %v\n", getOK, value)
		}
	case "set":
		if got, want := len(args), 3; got != want {
			return fmt.Errorf("unexpected number of arguments: got %v, want %v", got, want)
		}
		key, value := args[1], args[2]
		if err := client.Set(ctx, key, value); err != nil {
			fmt.Fprintf(env.Stdout, "%v %v\n", setFailed, verror.ErrorID(err))
		} else {
			fmt.Fprintf(env.Stdout, "%v\n", setOK)
		}
	}
	return nil
}, "client")

func startClient(t *v23tests.T, name string, args ...string) modules.Handle {
	creds, err := t.Shell().NewChildCredentials(name)
	if err != nil {
		t.Fatalf("NewChildCredentials(%v) failed: %v", name, err)
	}
	handle, err := t.Shell().StartWithOpts(
		t.Shell().DefaultStartOpts().WithCustomCredentials(creds).WithSessions(t, time.Minute),
		nil,
		runClient,
		args...,
	)
	if err != nil {
		t.Fatalf("StartWithOpts() failed: %v", err)
	}
	return handle
}

// V23TestGroupServerAuthorization uses an instance of the
// KeyValueStore server with an groups-based authorizer to test the
// group server implementation.
func V23TestGroupServerAuthorization(t *v23tests.T) {
	v23tests.RunRootMT(t, "--v23.tcp.address=127.0.0.1:0")

	// Build binaries for the groups client and server.
	var (
		clientBin  = t.BuildV23Pkg("v.io/x/ref/services/groups/groups")
		serverBin  = t.BuildV23Pkg("v.io/x/ref/services/groups/groupsd")
		serverName = "groups-server"
		readers    = naming.Join(serverName, "readers")
		writers    = naming.Join(serverName, "writers")
	)

	// Start the groups server.
	server := serverBin.Start("-name="+serverName, "-v23.tcp.address=127.0.0.1:0")

	// Create a couple of groups. The <readers> and <writers> groups
	// identify blessings that can be used to read from and write to the
	// key value store server respectively.
	clientBin.Start("create", readers, "root/alice", "root/bob").WaitOrDie(os.Stdout, os.Stderr)
	clientBin.Start("create", writers, "root/alice").WaitOrDie(os.Stdout, os.Stderr)

	// Start an instance of the key value store server.
	if _, err := t.Shell().Start(nil, runServer); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Test that alice can write.
	startClient(t, "alice", "set", "foo", "bar").Expect(setOK)
	// Test that alice can read.
	startClient(t, "alice", "get", "foo").Expectf("%v %v", getOK, "bar")
	// Test that bob can read.
	startClient(t, "bob", "get", "foo").Expectf("%v %v", getOK, "bar")
	// Test that bob cannot write.
	startClient(t, "bob", "set", "foo", "bar").Expectf("%v %v", setFailed, verror.ErrNoAccess.ID)
	// Stop the groups server and check that as a consequence "alice"
	// cannot read from the key value store server anymore.
	if err := server.Kill(syscall.SIGTERM); err != nil {
		t.Fatalf("Kill() failed: %v", err)
	}
	if err := server.Wait(os.Stdout, os.Stderr); err != nil {
		t.Fatalf("Wait() failed: %v", err)
	}
	startClient(t, "alice", "get", "foo").Expectf("%v %v", getFailed, verror.ErrNoAccess.ID)
}
