// A command-line tool to interface with the veyron namespace.
package main

import (
	"veyron/tools/namespace/impl"
	"veyron2/rt"
)

func main() {
	defer rt.Init().Shutdown()
	impl.Root().Main()
}
