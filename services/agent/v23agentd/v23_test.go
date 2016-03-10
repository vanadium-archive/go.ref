// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bufio"
	"os"
	"strconv"
	"testing"

	"v.io/x/ref"
	"v.io/x/ref/lib/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/agent/internal/constants"
	"v.io/x/ref/test/v23test"
)

func TestV23AgentdAllPrincipalMethods(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	sh.PropagateChildOutput = true
	defer sh.Cleanup()

	testbin := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/internal/test_principal")
	agentd := v23test.BuildGoPkg(sh, "v.io/x/ref/services/agent/v23agentd")

	credsDir := sh.MakeTempDir()
	principal, err := security.CreatePersistentPrincipal(credsDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := security.InitDefaultBlessings(principal, "happy"); err != nil {
		t.Fatal(err)
	}
	agentC := sh.Cmd(agentd, credsDir)
	agentRead, agentWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	agentC.ExtraFiles = append(agentC.ExtraFiles, agentWrite)
	pipeFD := 3 + len(agentC.ExtraFiles) - 1
	agentC.Vars["V23_AGENT_PARENT_PIPE_FD"] = strconv.Itoa(pipeFD)
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
	testC := sh.Cmd(testbin, "--expect-blessing=happy")
	testC.Vars[ref.EnvAgentPath] = constants.SocketPath(credsDir)
	testC.Run()
}

func TestMain(m *testing.M) {
	v23test.TestMain(m)
}
