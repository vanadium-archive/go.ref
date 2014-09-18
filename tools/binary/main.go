package main

import (
	"veyron.io/veyron/veyron/tools/binary/impl"

	"veyron.io/veyron/veyron2/rt"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()

	impl.Root().Main()
}
