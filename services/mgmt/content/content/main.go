package main

import (
	"veyron/services/mgmt/content/content/impl"

	"veyron2/rt"
)

func main() {
	r := rt.Init()
	defer r.Shutdown()

	impl.Root().Main()
}
