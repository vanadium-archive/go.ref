// The following enables go generate to generate the doc.go file.
//go:generate go run $VEYRON_ROOT/lib/cmdline/testdata/gendoc.go .

package main

import (
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/rt"

	_ "veyron.io/veyron/veyron/profiles"
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
