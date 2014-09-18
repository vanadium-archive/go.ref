package main

import (
	"veyron.io/veyron/veyron/tools/application/impl"

	"veyron.io/veyron/veyron2/rt"
)

func main() {
	defer rt.Init().Cleanup()
	impl.Root().Main()
}
