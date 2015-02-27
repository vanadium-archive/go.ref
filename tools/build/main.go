// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"os"

	"v.io/v23"
	"v.io/v23/context"

	_ "v.io/core/veyron/profiles"
)

var gctx *context.T

func main() {
	var shutdown v23.Shutdown
	gctx, shutdown = v23.Init()
	substituteVarsInFlags()
	exitCode := root().Main()
	shutdown()
	os.Exit(exitCode)
}
