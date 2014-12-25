// +build ignore

package main

import (
	"fmt"
	"v.io/veyron/veyron/lib/netstate"
)

func main() {
	al, err := netstate.GetAll()
	if err != nil {
		fmt.Printf("error getting networking state: %s", err)
		return
	}
	for _, a := range al {
		fmt.Println(a)
	}
}
