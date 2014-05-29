package main

import (
	"veyron/tools/proximity/impl"

	"veyron2/rt"
)

func main() {
	defer rt.Init().Shutdown()
	impl.Root().Main()
}
