package main

import (
	"veyron/tools/mounttable/impl"

	"veyron2/rt"
)

func main() {
	defer rt.Init().Cleanup()
	impl.Root().Main()
}
