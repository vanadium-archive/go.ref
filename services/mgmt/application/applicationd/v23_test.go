// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package main_test

import "testing"
import "os"

import "v.io/core/veyron/lib/testutil"
import "v.io/core/veyron/lib/testutil/v23tests"

func TestMain(m *testing.M) {
	testutil.Init()
	cleanup := v23tests.UseSharedBinDir()
	r := m.Run()
	cleanup()
	// TODO(cnicolaou): call modules.Dispatch and remove the need for TestHelperProcess
	os.Exit(r)
}

func TestV23ApplicationRepository(t *testing.T) {
	v23tests.RunTest(t, V23TestApplicationRepository)
}
