package main

import (
	"veyron/tools/application/impl"

	"veyron2/rt"
)

func main() {
	defer rt.Init().Shutdown()
	impl.Root().Main()
}
