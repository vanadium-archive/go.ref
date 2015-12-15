// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"v.io/v23/security"
	"v.io/x/ref"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/lib/v23test"
	"v.io/x/ref/test/expect"
)

func TestV23Agentd(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{Large: true})
	defer sh.Cleanup()

	var (
		clientDir, serverDir = createClientAndServerCredentials(t, sh)
		tmpdir               = sh.MakeTempDir()
		vrun                 = sh.JiriBuildGoPkg("v.io/x/ref/cmd/vrun")
		pingpong             = sh.JiriBuildGoPkg("v.io/x/ref/services/agent/internal/pingpong")
		agentd               = sh.JiriBuildGoPkg("v.io/x/ref/services/agent/agentd")
	)

	cmd := sh.Cmd(agentd, pingpong)
	cmd.Vars[ref.EnvCredentials] = serverDir
	session := expect.NewSession(t, cmd.StdoutPipe(), time.Minute)
	cmd.Start()
	serverName := session.ExpectVar("NAME")

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
		session := expect.NewSession(t, cmd.StdoutPipe(), time.Minute)
		cmd.Run()
		session.Expect("Pinging...")
		session.Expect(fmt.Sprintf("pong (client:[%v] server:[pingpongd])", test.Client))
	}
}

// createClientAndServerCredentials creates two principals, sets up their
// blessings and returns the created credentials directories.
//
// The server will have a single blessing "pingpongd".
// The client will have a single blessing "pingpongd:client", blessed by the
// server.
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
