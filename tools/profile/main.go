package main

import (
	"veyron/tools/profile/impl"

	"veyron2/rt"
)

func main() {
	r := rt.Init()
	defer r.Shutdown()

	impl.Root().Main()
}
