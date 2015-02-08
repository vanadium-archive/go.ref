// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package main_test

import "testing"
import "os"

import "v.io/core/veyron/lib/testutil"
import "v.io/core/veyron/lib/testutil/v23tests"

func TestMain(m *testing.M) {
	testutil.Init()
	// TODO(cnicolaou): call modules.Dispatch and remove the need for TestHelperProcess
	os.Exit(m.Run())
}

func TestV23BlessSelf(t *testing.T) {
	v23tests.RunTest(t, V23TestBlessSelf)
}
