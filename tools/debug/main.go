// A command-line tool to interface with the debug server.
package main

import (
	"veyron.io/veyron/veyron/tools/debug/impl"
	"veyron.io/veyron/veyron2/rt"
)

func main() {
	defer rt.Init().Cleanup()
	impl.Root().Main()
}
