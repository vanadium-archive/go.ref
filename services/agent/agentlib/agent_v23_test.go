// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agentlib_test

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"v.io/v23/security"
	"v.io/x/ref"
	vsecurity "v.io/x/ref/lib/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/agent/agentlib"
	"v.io/x/ref/services/agent/keymgr"
	"v.io/x/ref/test/v23test"
)

func start(c *v23test.Cmd) {
	c.S.SetVerbosity(true)
	c.Start()
}

func TestV23PassPhraseUse(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	defer sh.Cleanup()

	bin := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/agentd")
	dir := sh.MakeTempDir()

	// Create the passphrase
	c := sh.Cmd(bin, "echo", "Hello")
	c.Vars[ref.EnvCredentials] = dir
	c.SetStdinReader(strings.NewReader("PASSWORD"))
	start(c)
	c.S.ReadLine() // Skip over ...creating new key... message
	c.S.Expect("Hello")
	c.Wait()

	// Use it successfully
	c = sh.Cmd(bin, "echo", "Hello")
	c.Vars[ref.EnvCredentials] = dir
	c.SetStdinReader(strings.NewReader("PASSWORD"))
	start(c)
	c.S.Expect("Hello")
	c.Wait()

	// Provide a bad password
	c = sh.Cmd(bin, "echo", "Hello")
	c.Vars[ref.EnvCredentials] = dir
	c.SetStdinReader(strings.NewReader("BADPASSWORD"))
	c.ExitErrorIsOk = true
	stdout, stderr := c.StdoutStderr()
	if c.Err == nil {
		t.Fatalf("expected an error.STDOUT:%v\nSTDERR:%v\n", stdout, stderr)
	}
	if got, want := c.Err.Error(), "exit status 1"; got != want {
		t.Errorf("Got %q, want %q", got, want)
	}
	if got, want := stderr, "passphrase incorrect for decrypting private key"; !strings.Contains(got, want) {
		t.Errorf("Got %q, wanted it to contain %q", got, want)
	}
}

func TestV23AllPrincipalMethods(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	defer sh.Cleanup()

	// Test all methods of the principal interface.
	// (Errors are printed to STDERR)
	testbin := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/internal/test_principal")
	agentd := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/agentd")

	c := sh.Cmd(agentd, testbin)
	c.Vars[ref.EnvCredentials] = sh.MakeTempDir()
	c.Run()
}

func TestV23AgentProcesses(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	defer sh.Cleanup()

	// Setup two principals: One for the agent that runs the pingpong
	// server, one for the client.  Since the server uses the default
	// authorization policy, the client must have a blessing delegated from
	// the server.
	var (
		clientDir, serverDir = createClientAndServerCredentials(t, sh)
		pingpong             = v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/internal/pingpong")
		agentd               = v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/agentd")
	)

	server := sh.Cmd(agentd, pingpong)
	server.Vars[ref.EnvCredentials] = serverDir
	start(server)
	serverName := server.S.ExpectVar("NAME")

	// Run the client via an agent once.
	client := sh.Cmd(agentd, pingpong, serverName)
	client.Vars[ref.EnvCredentials] = clientDir
	start(client)
	client.S.Expect("Pinging...")
	client.S.Expect("pong (client:[pingpongd:client] server:[pingpongd])")
	client.Wait()

	// Run it through a shell to test that the agent can pass credentials
	// to subprocess of a shell (making things like "agentd bash" provide
	// useful terminals).
	// This only works with shells that propagate open file descriptors to
	// children. POSIX-compliant shells do this as to many other commonly
	// used ones like bash.
	script := filepath.Join(sh.MakeTempDir(), "test.sh")
	if err := writeScript(
		script,
		`#!/bin/bash
echo "Running client"
{{.Bin}} {{.Server}} || exit 101
echo "Running client again"
{{.Bin}} {{.Server}} || exit 102
`,
		struct{ Bin, Server string }{pingpong, serverName}); err != nil {
		t.Fatal(err)
	}
	client = sh.Cmd(agentd, "bash", script)
	client.Vars[ref.EnvCredentials] = clientDir
	start(client)
	client.S.Expect("Running client")
	client.S.Expect("Pinging...")
	client.S.Expect("pong (client:[pingpongd:client] server:[pingpongd])")
	client.S.Expect("Running client again")
	client.S.Expect("Pinging...")
	client.S.Expect("pong (client:[pingpongd:client] server:[pingpongd])")
	client.Wait()
}

func TestV23AgentRestartExitCode(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	defer sh.Cleanup()

	var (
		clientDir, serverDir = createClientAndServerCredentials(t, sh)
		pingpong             = v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/internal/pingpong")
		agentd               = v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/agentd")

		scriptDir = sh.MakeTempDir()
		counter   = filepath.Join(scriptDir, "counter")
		script    = filepath.Join(scriptDir, "test.sh")
	)

	server := sh.Cmd(agentd, pingpong)
	server.Vars[ref.EnvCredentials] = serverDir
	start(server)
	serverName := server.S.ExpectVar("NAME")

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
		t.Fatal(err)
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
			t.Fatalf("%q: %v", counter, err)
		}
		// Run the script under the agent
		cmd := sh.Cmd(agentd, "--restart-exit-code="+test.RestartExitCode, "bash", "-c", script)
		cmd.Vars[ref.EnvCredentials] = clientDir
		cmd.ExitErrorIsOk = true
		cmd.Run()
		var gotError string
		if cmd.Err != nil {
			gotError = cmd.Err.Error()
		}
		if got, want := gotError, test.WantError; got != want {
			t.Errorf("%+v: Got %q, want %q", test, got, want)
		}
		if buf, err := ioutil.ReadFile(counter); err != nil {
			t.Errorf("ioutil.ReadFile(%q) failed: %v", counter, err)
		} else if got, want := string(buf), test.WantCounter; got != want {
			t.Errorf("%+v: Got %q, want %q", test, got, want)
		}
	}
}

func TestV23KeyManager(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	defer sh.Cleanup()

	// Start up an agent that serves additional principals.
	// Since the agent needs to run some long-lived program to stay up,
	// use a script that waits for user input.
	var (
		agentDir, otherDir = sh.MakeTempDir(), sh.MakeTempDir()
		agentSock          = filepath.Join(agentDir, "agent.sock") // "agent.sock" comes from agentd/main.go
		agentd             = v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/agentd")
		clientSock         = filepath.Join(sh.MakeTempDir(), "client.sock")
		script             = filepath.Join(sh.MakeTempDir(), "test.sh")

		startAgent = func() (*v23test.Cmd, io.WriteCloser) {
			agent := sh.Cmd(agentd, "--additional-principals="+otherDir, "--no-passphrase", script)
			agent.Vars[ref.EnvCredentials] = agentDir
			wc := agent.StdinPipe()
			start(agent)
			for lineno := 0; lineno < 10; lineno++ {
				line := agent.S.ReadLine()
				if line == "STARTED" {
					return agent, wc
				}
				t.Logf("Ignoring line from agent process: %q", line)
			}
			t.Fatal("Agent did not start correctly?")
			return nil, nil
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
		t.Fatal(err)
	}
	agent, stdinPipe := startAgent()
	// Setup the principal for the other key
	kmgr, err := keymgr.NewKeyManager(agentSock)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := kmgr.NewPrincipal(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := kmgr.ServePrincipal(handle, clientSock); err != nil {
		t.Fatal(err)
	}
	// clientSock should be functional.
	if err := testClient(); err != nil {
		t.Fatal(err)
	}
	// If we restart the agent, and have it serve the client principal on the same
	// socket, the client should function again.
	stdinPipe.Close()
	agent.Terminate(os.Interrupt)
	t.Logf("Killed the agent, restarting it")
	agent, stdinPipe = startAgent()
	defer func() {
		stdinPipe.Close()
		agent.Terminate(os.Interrupt)
	}()
	// The bug that this test is trying to fix is one where the clientSock
	// file is left hanging around. At the time this test was written,
	// the author couldn't figure out a nice way to "uncleanly" kill the agent
	// and thus cause this problem. Anyway, ensure that this test is testing
	// the issue it was intended to by checking that clientSock is still lying
	// around.
	if _, err := os.Stat(clientSock); err != nil {
		t.Fatal(err)
	}
	// TODO(ashankar,ribrdb): To make clients resilient to the server
	// listening on the unix socket restarting, should ipc.IPCConn or the
	// KeyManager transparently retry when the connection has died instead
	// of failing?
	if kmgr, err = keymgr.NewKeyManager(agentSock); err != nil {
		t.Fatal(err)
	}
	if err := kmgr.ServePrincipal(handle, clientSock); err != nil {
		t.Fatal(err)
	}
	if err := testClient(); err != nil {
		t.Fatal(err)
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

// createClientAndServerCredentials creates two principals, sets up their
// blessings and returns the created credentials directories.
//
// The server will have a single blessing "pingpongd".
// The client will have a single blessing "pingpongd:client", blessed by the
// server.
//
// Nearly identical to createClientAndServerCredentials in
// v.io/x/ref/cmd/vrun/vrun_v23_test.go.
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
	return
}
