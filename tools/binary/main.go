package main

import (
	"veyron/tools/binary/impl"

	"veyron2/rt"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()

	impl.Root().Main()
}
