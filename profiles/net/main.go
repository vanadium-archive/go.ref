// +build ignore

package main

import (
	"fmt"

	"veyron2/config"
	"veyron2/rt"

	"veyron/profiles/net"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()

	ch := make(chan config.Setting, 10)
	p := r.Publisher()
	settings, err := p.ForkStream(net.StreamName, ch)
	if err != nil {
		r.Logger().Infof("failed to fork stream: %s", err)
	}
	for _, setting := range settings.Latest {
		fmt.Println("Setting: ", setting)
	}
	for setting := range ch {
		fmt.Println("Setting: ", setting)
	}
}
