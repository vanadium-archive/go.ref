// +build ignore

package main

import (
	"fmt"

	"veyron2/rt"

	_ "veyron/profiles/net/bluetooth"
)

func main() {
	r := rt.Init()
	p := r.Profile()
	fmt.Printf("Profile %q, %s\n", p.Name(), p)
}
