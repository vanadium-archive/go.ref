// A command-line tool to interface with the veyron namespace.
package main

import (
	"veyron.io/veyron/veyron/tools/namespace/impl"
	"veyron.io/veyron/veyron2/rt"
)

func main() {
	defer rt.Init().Cleanup()
	impl.Root().Main()
}
