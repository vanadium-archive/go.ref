// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"os"

	"v.io/v23"

	_ "v.io/x/ref/profiles/static"
	"v.io/x/ref/tools/mgmt/device/impl"
)

func main() {
	gctx, shutdown := v23.Init()
	impl.SetGlobalContext(gctx)
	exitCode := impl.Root().Main()
	shutdown()
	os.Exit(exitCode)
}
