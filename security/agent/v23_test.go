// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package agent_test

import "testing"
import "os"

import "v.io/core/veyron/lib/testutil"
import "v.io/core/veyron/lib/testutil/v23tests"

func TestMain(m *testing.M) {
	testutil.Init()
	cleanup := v23tests.UseSharedBinDir()
	r := m.Run()
	cleanup()
	os.Exit(r)
}

func TestV23TestPassPhraseUse(t *testing.T) {
	v23tests.RunTest(t, V23TestTestPassPhraseUse)
}

func TestV23AllPrincipalMethods(t *testing.T) {
	v23tests.RunTest(t, V23TestAllPrincipalMethods)
}

func TestV23AgentProcesses(t *testing.T) {
	v23tests.RunTest(t, V23TestAgentProcesses)
}

func TestV23AgentRestart(t *testing.T) {
	v23tests.RunTest(t, V23TestAgentRestart)
}
