// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

package agentlib_test

import (
	"os"
	"testing"

	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

func TestMain(m *testing.M) {
	modules.DispatchAndExitIfChild()
	cleanup := v23tests.UseSharedBinDir()
	r := m.Run()
	cleanup()
	os.Exit(r)
}

func TestV23PassPhraseUse(t *testing.T) {
	v23tests.RunTest(t, V23TestPassPhraseUse)
}

func TestV23AllPrincipalMethods(t *testing.T) {
	v23tests.RunTest(t, V23TestAllPrincipalMethods)
}

func TestV23AgentProcesses(t *testing.T) {
	v23tests.RunTest(t, V23TestAgentProcesses)
}

func TestV23AgentRestartExitCode(t *testing.T) {
	v23tests.RunTest(t, V23TestAgentRestartExitCode)
}

func TestV23KeyManager(t *testing.T) {
	v23tests.RunTest(t, V23TestKeyManager)
}
