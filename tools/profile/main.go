// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/tools/go/src/tools/lib/cmdline/testdata/gendoc.go .

package main

import (
	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/rt"

	_ "v.io/veyron/veyron/profiles"
)

var runtime veyron2.Runtime

func main() {
	var err error
	runtime, err = rt.New()
	if err != nil {
		panic(err)
	}
	defer runtime.Cleanup()
	root().Main()
}
