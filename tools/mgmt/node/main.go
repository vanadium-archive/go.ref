package main

import (
	"veyron.io/veyron/veyron2/rt"

	_ "veyron.io/veyron/veyron/profiles"
)

func main() {
	defer rt.Init().Cleanup()
	root().Main()
}
