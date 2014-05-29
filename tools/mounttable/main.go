package main

import (
	"veyron/tools/mounttable/impl"

	"veyron2/rt"
)

func main() {
	defer rt.Init().Shutdown()
	impl.Root().Main()
}
