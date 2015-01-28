// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/release/go/src/v.io/lib/cmdline/testdata/gendoc.go .

package main

import (
	"os"

	"v.io/core/veyron2"

	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron/tools/mgmt/device/impl"
)

func main() {
	gctx, shutdown := veyron2.Init()
	impl.SetGlobalContext(gctx)
	exitCode := impl.Root().Main()
	shutdown()
	os.Exit(exitCode)
}
