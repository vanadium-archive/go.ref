package main

import (
	"veyron/services/mounttable/mounttable/impl"

	"veyron2/rt"
)

func main() {
	defer rt.Init().Shutdown()
	impl.Root().Main()
}
