// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"fmt"
	"os"
	"testing"

	"v.io/v23/security"
	"v.io/x/ref"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/test/v23test"
)

func TestV23Agentd(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	var (
		clientDir, serverDir = createClientAndServerCredentials(t, sh)
		tmpdir               = sh.MakeTempDir()
		vrun                 = sh.BuildGoPkg("v.io/x/ref/cmd/vrun")
		pingpong             = sh.BuildGoPkg("v.io/x/ref/services/agent/internal/pingpong")
		agentd               = sh.BuildGoPkg("v.io/x/ref/services/agent/agentd")
	)

	cmd := sh.Cmd(agentd, pingpong)
	cmd.Vars[ref.EnvCredentials] = serverDir
	cmd.Start()
	serverName := cmd.S.ExpectVar("NAME")

	tests := []struct {
		Command []string
		Client  string
	}{
		{
			[]string{pingpong, serverName}, // No vrun
			"pingpongd:client",
		},
		{
			[]string{vrun, "--i-really-need-vrun", pingpong, serverName},
			"pingpongd:client:pingpong",
		},
		{
			[]string{vrun, "--i-really-need-vrun", "--name=bugsy", pingpong, serverName},
			"pingpongd:client:bugsy",
		},
	}
	for _, test := range tests {
		args := append([]string{"--additional-principals=" + tmpdir}, test.Command...)
		cmd := sh.Cmd(agentd, args...)
		cmd.Vars[ref.EnvCredentials] = clientDir
		cmd.Run()
		cmd.S.Expect("Pinging...")
		cmd.S.Expect(fmt.Sprintf("pong (client:[%v] server:[pingpongd])", test.Client))
	}
}

// createClientAndServerCredentials creates two principals, sets up their
// blessings and returns the created credentials directories.
//
// The server will have a single blessing "pingpongd".
// The client will have a single blessing "pingpongd:client", blessed by the
// server.
//
// Nearly identical to createClientAndServerCredentials in
// v.io/x/ref/services/agent/agentlib/agent_v23_test.go.
func createClientAndServerCredentials(t *testing.T, sh *v23test.Shell) (clientDir, serverDir string) {
	clientDir = sh.MakeTempDir()
	serverDir = sh.MakeTempDir()

	pserver, err := vsecurity.CreatePersistentPrincipal(serverDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	pclient, err := vsecurity.CreatePersistentPrincipal(clientDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Server will only serve, not make any client requests, so only needs a
	// default blessing.
	bserver, err := pserver.BlessSelf("pingpongd")
	if err != nil {
		t.Fatal(err)
	}
	if err := pserver.BlessingStore().SetDefault(bserver); err != nil {
		t.Fatal(err)
	}
	// Clients need not have a default blessing as they will only make a request
	// to the server.
	bclient, err := pserver.Bless(pclient.PublicKey(), bserver, "client", security.UnconstrainedUse())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pclient.BlessingStore().Set(bclient, "pingpongd"); err != nil {
		t.Fatal(err)
	}
	// The client and server must both recognize bserver and its delegates.
	if err := security.AddToRoots(pserver, bserver); err != nil {
		t.Fatal(err)
	}
	if err := security.AddToRoots(pclient, bserver); err != nil {
		t.Fatal(err)
	}
	// We need to add set a default blessings to this pclient for this principal
	// to work with vrun.
	if err := pclient.BlessingStore().SetDefault(bclient); err != nil {
		t.Fatal(err)
	}
	return
}

func TestMain(m *testing.M) {
	os.Exit(v23test.Run(m.Run))
}
