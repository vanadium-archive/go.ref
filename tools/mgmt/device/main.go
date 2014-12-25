// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/veyron/go/src/v.io/lib/cmdline/testdata/gendoc.go .

package main

import (
	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/rt"

	_ "v.io/veyron/veyron/profiles"
)

var runtime veyron2.Runtime

func main() {
	var err error
	if runtime, err = rt.New(); err != nil {
		panic(err)
	}
	defer runtime.Cleanup()
	root().Main()
}
