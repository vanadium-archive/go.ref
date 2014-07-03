package main

import (
	"veyron/tools/application/impl"

	"veyron2/rt"
)

func main() {
	defer rt.Init().Cleanup()
	impl.Root().Main()
}
