// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package main_test

import "fmt"
import "testing"
import "os"

import "v.io/core/veyron/lib/modules"
import "v.io/core/veyron/lib/testutil"
import "v.io/core/veyron/lib/testutil/v23tests"

func init() {
	modules.RegisterChild("dummy", `HACK: This is a hack to force v23 test generate to generate modules.Dispatch in TestMain.
TODO(suharshs,cnicolaou): Find a way to get rid of this dummy subprocesses.`, dummy)
}

func TestMain(m *testing.M) {
	testutil.Init()
	if modules.IsModulesProcess() {
		if err := modules.Dispatch(); err != nil {
			fmt.Fprintf(os.Stderr, "modules.Dispatch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	cleanup := v23tests.UseSharedBinDir()
	r := m.Run()
	cleanup()
	os.Exit(r)
}

func TestV23Simulator(t *testing.T) {
	v23tests.RunTest(t, V23TestSimulator)
}
