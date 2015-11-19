// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	lsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

//go:generate jiri test generate

func V23TestClaimableServer(t *v23tests.T) {
	workdir, err := ioutil.TempDir("", "claimable-test-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(workdir)

	permsDir := filepath.Join(workdir, "perms")

	serverCreds, err := detachedCredentials(t, "server")
	if err != nil {
		t.Fatalf("Failed to create server credentials: %v", err)
	}
	legitClientCreds, err := t.Shell().NewChildCredentials("legit")
	if err != nil {
		t.Fatalf("Failed to create legit credentials: %v", err)
	}
	badClientCreds1, err := t.Shell().NewCustomCredentials()
	if err != nil {
		t.Fatalf("Failed to create bad credentials: %v", err)
	}
	badClientCreds2, err := t.Shell().NewChildCredentials("other-guy")
	if err != nil {
		t.Fatalf("Failed to create bad credentials: %v", err)
	}

	serverBin := t.BuildV23Pkg("v.io/x/ref/services/device/claimable")
	serverBin = serverBin.WithStartOpts(serverBin.StartOpts().WithCustomCredentials(serverCreds))

	server := serverBin.Start(
		"--v23.tcp.address=127.0.0.1:0",
		"--perms-dir="+permsDir,
		"--root-blessings="+rootBlessings(t, legitClientCreds),
		"--v23.permissions.literal={\"Admin\":{\"In\":[\"root:legit\"]}}",
	)
	addr := server.ExpectVar("NAME")

	clientBin := t.BuildV23Pkg("v.io/x/ref/services/device/device")

	testcases := []struct {
		creds      *modules.CustomCredentials
		success    bool
		permsExist bool
	}{
		{badClientCreds1, false, false},
		{badClientCreds2, false, false},
		{legitClientCreds, true, true},
	}

	for _, tc := range testcases {
		clientBin = clientBin.WithStartOpts(clientBin.StartOpts().WithCustomCredentials(tc.creds))
		client := clientBin.Start("claim", addr, "my-device")
		if err := client.Wait(nil, nil); (err == nil) != tc.success {
			t.Errorf("Unexpected exit value. Expected success=%v, got err=%v", tc.success, err)
		}
		if _, err := os.Stat(permsDir); (err == nil) != tc.permsExist {
			t.Errorf("Unexpected permsDir state. Got %v, expected %v", err == nil, tc.permsExist)
		}
	}
	// Server should exit cleanly after the successful Claim.
	if err := server.ExpectEOF(); err != nil {
		t.Errorf("Expected server to exit cleanly, got %v", err)
	}
}

func detachedCredentials(t *v23tests.T, name string) (*modules.CustomCredentials, error) {
	creds, err := t.Shell().NewCustomCredentials()
	if err != nil {
		return nil, err
	}
	return creds, lsecurity.InitDefaultBlessings(creds.Principal(), name)
}

func rootBlessings(t *v23tests.T, creds *modules.CustomCredentials) string {
	principalBin := t.BuildV23Pkg("v.io/x/ref/cmd/principal")
	opts := principalBin.StartOpts().WithCustomCredentials(creds)
	principalBin = principalBin.WithStartOpts(opts)
	blessings := strings.TrimSpace(principalBin.Start("get", "default").Output())

	opts.Stdin = bytes.NewBufferString(blessings)
	opts.ExecProtocol = false
	opts.Credentials = nil
	principalBin = principalBin.WithStartOpts(opts)
	out := strings.TrimSpace(principalBin.Start("dumproots", "-").Output())
	return strings.Replace(out, "\n", ",", -1)
}
