// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/release/go/src/v.io/lib/cmdline/testdata/gendoc.go .

package main

import (
	"os"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"

	_ "v.io/core/veyron/profiles"
)

var gctx *context.T

func main() {
	var shutdown veyron2.Shutdown
	gctx, shutdown = veyron2.Init()
	substituteVarsInFlags()
	exitCode := root().Main()
	shutdown()
	os.Exit(exitCode)
}
