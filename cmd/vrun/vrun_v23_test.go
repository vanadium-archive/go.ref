// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

//go:generate jiri test generate .

import (
	"fmt"
	"os"

	"v.io/v23/security"
	"v.io/x/ref"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/test/v23tests"
)

func V23TestAgentd(t *v23tests.T) {
	var (
		clientAgent, serverAgent = createClientAndServerAgents(t)
		tmpdir                   = t.NewTempDir("")
		vrun                     = t.BuildGoPkg("v.io/x/ref/cmd/vrun").Path()
		pingpong                 = t.BuildGoPkg("v.io/x/ref/services/agent/internal/pingpong").Path()
		serverName               = serverAgent.Start(pingpong).ExpectVar("NAME")

		tests = []struct {
			Command []string
			Client  string
		}{
			{
				[]string{pingpong, serverName}, // No vrun
				"pingpongd:client",
			},
			{
				[]string{vrun, pingpong, serverName},
				"pingpongd:client:pingpong",
			},
			{
				[]string{vrun, "--name=bugsy", pingpong, serverName},
				"pingpongd:client:bugsy",
			},
		}
	)
	for _, test := range tests {
		args := append([]string{"--additional-principals=" + tmpdir}, test.Command...)
		client := clientAgent.Start(args...)
		client.Expect("Pinging...")
		client.Expect(fmt.Sprintf("pong (client:[%v] server:[pingpongd])", test.Client))
		client.WaitOrDie(os.Stdout, os.Stderr)
		if err := client.Error(); err != nil {
			t.Errorf("Test: %+v: %v", test, err)
		}
	}
}

// createClientAndServerAgents creates two principals, sets up their
// blessings and returns the agent binaries that will use the created credentials.
//
// The server will have a single blessing "pingpongd".
// The client will have a single blessing "pingpongd:client", blessed by the server.
func createClientAndServerAgents(i *v23tests.T) (client, server *v23tests.Binary) {
	var (
		agentd    = i.BuildGoPkg("v.io/x/ref/services/agent/agentd")
		clientDir = i.NewTempDir("")
		serverDir = i.NewTempDir("")
	)
	pserver, err := vsecurity.CreatePersistentPrincipal(serverDir, nil)
	if err != nil {
		i.Fatal(err)
	}
	pclient, err := vsecurity.CreatePersistentPrincipal(clientDir, nil)
	if err != nil {
		i.Fatal(err)
	}
	// Server will only serve, not make any client requests, so only needs a default blessing.
	bserver, err := pserver.BlessSelf("pingpongd")
	if err != nil {
		i.Fatal(err)
	}
	if err := pserver.BlessingStore().SetDefault(bserver); err != nil {
		i.Fatal(err)
	}
	// Clients need not have a default blessing as they will only make a request to the server.
	bclient, err := pserver.Bless(pclient.PublicKey(), bserver, "client", security.UnconstrainedUse())
	if err != nil {
		i.Fatal(err)
	}
	if _, err := pclient.BlessingStore().Set(bclient, "pingpongd"); err != nil {
		i.Fatal(err)
	}
	// The client and server must both recognize bserver and its delegates.
	if err := security.AddToRoots(pserver, bserver); err != nil {
		i.Fatal(err)
	}
	if err := security.AddToRoots(pclient, bserver); err != nil {
		i.Fatal(err)
	}
	// We need to add set a default blessings to this pclient for this principal to
	// work with vrun.
	if err := pclient.BlessingStore().SetDefault(bclient); err != nil {
		i.Fatal(err)
	}
	return agentd.WithEnv(ref.EnvCredentials + "=" + clientDir), agentd.WithEnv(ref.EnvCredentials + "=" + serverDir)
}
