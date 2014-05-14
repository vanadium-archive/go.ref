package main

import (
	"veyron/services/mounttable/mounttable/impl"

	"veyron2/rt"
)

func main() {
	r := rt.Init()
	defer r.Shutdown()

	impl.Root().Main()
}
