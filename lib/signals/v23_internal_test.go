// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package signals

import "fmt"
import "testing"
import "os"

import "v.io/core/veyron/lib/modules"
import "v.io/core/veyron/lib/testutil"

func init() {
	modules.RegisterChild("handleDefaults", ``, handleDefaults)
	modules.RegisterChild("handleCustom", ``, handleCustom)
	modules.RegisterChild("handleCustomWithStop", ``, handleCustomWithStop)
	modules.RegisterChild("handleDefaultsIgnoreChan", ``, handleDefaultsIgnoreChan)
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
	os.Exit(m.Run())
}
