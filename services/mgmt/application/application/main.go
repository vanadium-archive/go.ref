package main

import (
	"veyron/services/mgmt/application/application/impl"

	"veyron2/rt"
)

func main() {
	defer rt.Init().Shutdown()
	impl.Root().Main()
}
