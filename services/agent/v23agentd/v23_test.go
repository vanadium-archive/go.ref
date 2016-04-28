// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"v.io/x/ref"
	"v.io/x/ref/lib/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/agent/internal/constants"
	"v.io/x/ref/test/v23test"
)

func upComesAgentd(t *testing.T, sh *v23test.Shell, credsDir, password string) {
	agentd := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/v23agentd")

	agentC := sh.Cmd(agentd, credsDir)
	if len(password) > 0 {
		agentC.SetStdinReader(strings.NewReader(password))
	}
	agentRead, agentWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	agentC.ExtraFiles = append(agentC.ExtraFiles, agentWrite)
	pipeFD := 3 + len(agentC.ExtraFiles) - 1
	agentC.Vars[constants.EnvAgentParentPipeFD] = strconv.Itoa(pipeFD)
	agentC.Vars["PATH"] = os.Getenv("PATH")
	agentC.Start()
	agentWrite.Close()
	scanner := bufio.NewScanner(agentRead)
	if !scanner.Scan() || scanner.Text() != constants.ServingMsg {
		t.Fatalf("Failed to receive \"%s\" from agent", constants.ServingMsg)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading standard input: %v", err)
	}
	agentRead.Close()
}

func TestV23UnencryptedPrincipal(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	sh.PropagateChildOutput = true
	defer sh.Cleanup()

	testbin := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/internal/test_principal")

	credsDir := sh.MakeTempDir()
	principal, err := security.CreatePersistentPrincipal(credsDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := security.InitDefaultBlessings(principal, "happy"); err != nil {
		t.Fatal(err)
	}

	const noPassword = ""
	upComesAgentd(t, sh, credsDir, noPassword)

	testC := sh.Cmd(testbin, "--expect-blessing=happy")
	testC.Vars[ref.EnvAgentPath] = constants.SocketPath(credsDir)
	testC.Run()
}

func TestV23EncryptedPrincipal(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	sh.PropagateChildOutput = true
	defer sh.Cleanup()

	testbin := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/internal/test_principal")

	credsDir := sh.MakeTempDir()
	principal, err := security.CreatePersistentPrincipal(credsDir, []byte("PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	if err := security.InitDefaultBlessings(principal, "sneezy"); err != nil {
		t.Fatal(err)
	}

	upComesAgentd(t, sh, credsDir, "PASSWORD")

	testC := sh.Cmd(testbin, "--expect-blessing=sneezy")
	testC.Vars[ref.EnvAgentPath] = constants.SocketPath(credsDir)
	testC.Run()
}

func TestMain(m *testing.M) {
	v23test.TestMain(m)
}

func upComesAgentdDaemon(t *testing.T, sh *v23test.Shell, credsDir, password string) {
	agentd := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/v23agentd")

	agentC := sh.Cmd(agentd, "--daemon", credsDir)
	if len(password) > 0 {
		agentC.SetStdinReader(strings.NewReader(password))
	}
	agentC.Run()
}

func downComesAgentd(t *testing.T, sh *v23test.Shell, credsDir string) {
	agentd := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/v23agentd")

	agentC := sh.Cmd(agentd, "--stop", credsDir)
	agentC.Run()
}

func TestV23Daemon(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	sh.PropagateChildOutput = true
	defer sh.Cleanup()

	testbin := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/internal/test_principal")

	credsDir := sh.MakeTempDir()
	principal, err := security.CreatePersistentPrincipal(credsDir, []byte("P@SsW0rd"))
	if err != nil {
		t.Fatal(err)
	}
	if err := security.InitDefaultBlessings(principal, "grumpy"); err != nil {
		t.Fatal(err)
	}

	upComesAgentdDaemon(t, sh, credsDir, "P@SsW0rd")

	testC := sh.Cmd(testbin, "--expect-blessing=grumpy")
	testC.Vars[ref.EnvAgentPath] = constants.SocketPath(credsDir)
	testC.Run()

	// Since we started the agent as a daemon, we need to explicitly stop it
	// (since the Shell won't do it for us).
	downComesAgentd(t, sh, credsDir)
}
