// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agentlib_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"v.io/v23/security"
	"v.io/x/ref"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/services/agent/agentlib"
	"v.io/x/ref/services/agent/keymgr"
	"v.io/x/ref/test/v23tests"
)

//go:generate jiri test generate

func V23TestPassPhraseUse(i *v23tests.T) {
	bin := i.BuildGoPkg("v.io/x/ref/services/agent/agentd").WithEnv(ref.EnvCredentials + "=" + i.NewTempDir(""))

	// Create the passphrase
	agent := bin.Start("echo", "Hello")
	fmt.Fprintln(agent.Stdin(), "PASSWORD")
	agent.ReadLine() // Skip over ...creating new key... message
	agent.Expect("Hello")
	agent.ExpectEOF()
	if err := agent.Error(); err != nil {
		i.Fatal(err)
	}

	// Use it successfully
	agent = bin.Start("echo", "Hello")
	fmt.Fprintln(agent.Stdin(), "PASSWORD")
	agent.Expect("Hello")
	agent.ExpectEOF()
	if err := agent.Error(); err != nil {
		i.Fatal(err)
	}

	// Provide a bad password
	agent = bin.Start("echo", "Hello")
	fmt.Fprintln(agent.Stdin(), "BADPASSWORD")
	agent.ExpectEOF()
	var stdout, stderr bytes.Buffer
	err := agent.Wait(&stdout, &stderr)
	if err == nil {
		i.Fatalf("expected an error.STDOUT:%v\nSTDERR:%v\n", stdout.String(), stderr.String())
	}
	if got, want := err.Error(), "exit status 1"; got != want {
		i.Errorf("Got %q, want %q", got, want)
	}
	if got, want := stderr.String(), "passphrase incorrect for decrypting private key"; !strings.Contains(got, want) {
		i.Errorf("Got %q, wanted it to contain %q", got, want)
	}
	if err := agent.Error(); err != nil {
		i.Error(err)
	}
}

func V23TestAllPrincipalMethods(i *v23tests.T) {
	// Test all methods of the principal interface.
	// (Errors are printed to STDERR)
	testbin := i.BuildGoPkg("v.io/x/ref/services/agent/internal/test_principal").Path()
	i.BuildGoPkg("v.io/x/ref/services/agent/agentd").
		WithEnv(ref.EnvCredentials+"="+i.NewTempDir("")).
		Start(testbin).
		WaitOrDie(nil, os.Stderr)
}

func V23TestAgentProcesses(i *v23tests.T) {
	// Setup two principals: One for the agent that runs the pingpong
	// server, one for the client.  Since the server uses the default
	// authorization policy, the client must have a blessing delegated from
	// the server.
	var (
		clientAgent, serverAgent = createClientAndServerAgents(i)
		pingpong                 = i.BuildGoPkg("v.io/x/ref/services/agent/internal/pingpong").Path()
		serverName               = serverAgent.Start(pingpong).ExpectVar("NAME")
	)
	// Run the client via an agent once.
	client := clientAgent.Start(pingpong, serverName)
	client.Expect("Pinging...")
	client.Expect("pong (client:[pingpongd/client] server:[pingpongd])")
	client.WaitOrDie(os.Stdout, os.Stderr)
	if err := client.Error(); err != nil { // Check expectations
		i.Fatal(err)
	}

	// Run it through a shell to test that the agent can pass credentials
	// to subprocess of a shell (making things like "agentd bash" provide
	// useful terminals).
	// This only works with shells that propagate open file descriptors to
	// children. POSIX-compliant shells do this as to many other commonly
	// used ones like bash.
	script := filepath.Join(i.NewTempDir(""), "test.sh")
	if err := writeScript(
		script,
		`#!/bin/bash
echo "Running client"
{{.Bin}} {{.Server}} || exit 101
echo "Running client again"
{{.Bin}} {{.Server}} || exit 102
`,
		struct{ Bin, Server string }{pingpong, serverName}); err != nil {
		i.Fatal(err)
	}
	client = clientAgent.Start("bash", script)
	client.Expect("Running client")
	client.Expect("Pinging...")
	client.Expect("pong (client:[pingpongd/client] server:[pingpongd])")
	client.Expect("Running client again")
	client.Expect("Pinging...")
	client.Expect("pong (client:[pingpongd/client] server:[pingpongd])")
	client.WaitOrDie(os.Stdout, os.Stderr)
	if err := client.Error(); err != nil { // Check expectations
		i.Fatal(err)
	}
}

func V23TestAgentRestartExitCode(i *v23tests.T) {
	var (
		clientAgent, serverAgent = createClientAndServerAgents(i)
		pingpong                 = i.BuildGoPkg("v.io/x/ref/services/agent/internal/pingpong").Path()
		serverName               = serverAgent.Start(pingpong).ExpectVar("NAME")

		scriptDir = i.NewTempDir("")
		counter   = filepath.Join(scriptDir, "counter")
		script    = filepath.Join(scriptDir, "test.sh")
	)
	if err := writeScript(
		script,
		`#!/bin/bash
# Execute the client ping once
{{.Bin}} {{.Server}} || exit 101
# Increment the contents of the counter file
readonly COUNT=$(expr $(<"{{.Counter}}") + 1)
echo -n $COUNT >{{.Counter}}
# Exit code is 0 if the counter is less than 5
[[ $COUNT -lt 5 ]]; exit $?
`,
		struct{ Bin, Server, Counter string }{
			Bin:     pingpong,
			Server:  serverName,
			Counter: counter,
		}); err != nil {
		i.Fatal(err)
	}

	tests := []struct {
		RestartExitCode string
		WantError       string
		WantCounter     string
	}{
		{
			// With --restart-exit-code=0, the script should be kicked off
			// 5 times till the counter reaches 5 and the last iteration of
			// the script will exit.
			RestartExitCode: "0",
			WantError:       "exit status 1",
			WantCounter:     "5",
		},
		{
			// With --restart-exit-code=!0, the script will be executed only once.
			RestartExitCode: "!0",
			WantError:       "",
			WantCounter:     "1",
		},
		{
			// --restart-exit-code=!1, should be the same
			// as --restart-exit-code=0 for this
			// particular script only exits with 0 or 1
			RestartExitCode: "!1",
			WantError:       "exit status 1",
			WantCounter:     "5",
		},
	}
	for _, test := range tests {
		// Clear out the counter file.
		if err := ioutil.WriteFile(counter, []byte("0"), 0644); err != nil {
			i.Fatalf("%q: %v", counter, err)
		}
		// Run the script under the agent
		var gotError string
		if err := clientAgent.Start(
			"--restart-exit-code="+test.RestartExitCode,
			"bash", "-c", script).
			Wait(os.Stdout, os.Stderr); err != nil {
			gotError = err.Error()
		}
		if got, want := gotError, test.WantError; got != want {
			i.Errorf("%+v: Got %q, want %q", test, got, want)
		}
		if buf, err := ioutil.ReadFile(counter); err != nil {
			i.Errorf("ioutil.ReadFile(%q) failed: %v", counter, err)
		} else if got, want := string(buf), test.WantCounter; got != want {
			i.Errorf("%+v: Got %q, want %q", test, got, want)
		}
	}
}

func V23TestKeyManager(i *v23tests.T) {
	// Start up an agent that serves additional principals.
	// Since the agent needs to run some long-lived program to stay up,
	// use a script that waits for user input.
	var (
		agentDir   = i.NewTempDir("")
		otherDir   = i.NewTempDir("")
		agentSock  = filepath.Join(agentDir, "agent.sock") // "agent.sock" comes from agentd/main.go
		agentBin   = i.BuildGoPkg("v.io/x/ref/services/agent/agentd").WithEnv(ref.EnvCredentials + "=" + agentDir)
		clientSock = filepath.Join(i.NewTempDir(""), "client.sock")
		script     = filepath.Join(i.NewTempDir(""), "test.sh")

		startAgent = func() *v23tests.Invocation {
			agent := agentBin.Start(
				"--additional-principals="+otherDir,
				"--no-passphrase",
				script)
			for lineno := 0; lineno < 10; lineno++ {
				line := agent.ReadLine()
				if line == "STARTED" {
					return agent
				}
				i.Logf("Ignoring line from agent process: %q", line)
			}
			i.Fatal("Agent did not start correctly?")
			return nil
		}

		testClient = func() error {
			p, err := agentlib.NewAgentPrincipalX(clientSock)
			if err != nil {
				return err
			}
			defer p.Close()
			_, err = p.BlessSelf("foo")
			return err
		}
	)
	if err := writeScript(
		script,
		`#!/bin/bash
echo STARTED
read
`,
		nil); err != nil {
		i.Fatal(err)
	}
	agent := startAgent()
	// Setup the principal for the other key
	kmgr, err := keymgr.NewKeyManager(agentSock)
	if err != nil {
		i.Fatal(err)
	}
	handle, err := kmgr.NewPrincipal(false)
	if err != nil {
		i.Fatal(err)
	}
	if err := kmgr.ServePrincipal(handle, clientSock); err != nil {
		i.Fatal(err)
	}
	// clientSock should be functional.
	if err := testClient(); err != nil {
		i.Fatal(err)
	}
	// If we restart the agent, and have it serve the client principal on the same socket,
	// the client should function again.
	agent.Shutdown(os.Stdout, os.Stderr)
	i.Logf("Killed the agent, restarting it")
	agent = startAgent()
	defer agent.Shutdown(os.Stdout, os.Stderr)
	// The bug that this test is trying to fix is one where the clientSock
	// file is left hanging around. At the time this test was written,
	// the author couldn't figure out a nice way to "uncleanly" kill the agent
	// and thus cause this problem. Anyway, ensure that this test is testing
	// the issue it was intended to by checking that clientSock is still lying
	// around.
	if _, err := os.Stat(clientSock); err != nil {
		i.Fatal(err)
	}
	// TODO(ashankar,ribrdb): To make clients resilient to the server
	// listening on the unix socket restarting, should ipc.IPCConn or the
	// KeyManager transparently retry when the connection has died instead
	// of failing?
	if kmgr, err = keymgr.NewKeyManager(agentSock); err != nil {
		i.Fatal(err)
	}
	if err := kmgr.ServePrincipal(handle, clientSock); err != nil {
		i.Fatal(err)
	}
	if err := testClient(); err != nil {
		i.Fatal(err)
	}
}

func writeScript(dstfile, tmpl string, args interface{}) error {
	t, err := template.New(dstfile).Parse(tmpl)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, args); err != nil {
		return err
	}
	if err := ioutil.WriteFile(dstfile, buf.Bytes(), 0700); err != nil {
		return err
	}
	return nil
}

// createClientAndServerAgents creates two principals, sets up their
// blessings and returns the agent binaries that will use the created credentials.
//
// The server will have a single blessing "pingpongd".
// The client will have a single blessing "pingpongd/client", blessed by the server.
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
	return agentd.WithEnv(ref.EnvCredentials + "=" + clientDir), agentd.WithEnv(ref.EnvCredentials + "=" + serverDir)
}
