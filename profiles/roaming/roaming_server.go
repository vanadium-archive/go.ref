// +build ignore

package main

import (
	"fmt"

	"veyron2/ipc"
	"veyron2/rt"

	"veyron/profiles/roaming"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()
	log := r.Logger()

	server, err := r.NewServer()
	defer server.Stop()
	if err != nil {
		log.Fatalf("unexpected error: %q", err)
	}

	ep, err := server.RoamingListen(r.Publisher(), roaming.SettingsStreamName)
	if err != nil {
		log.Fatalf("unexpected error: %q", err)
	}
	if ep != nil {
		fmt.Println(ep)
	}
	if err := server.Serve("roamer", ipc.LeafDispatcher(&dispatcher{}, nil)); err != nil {
		log.Fatalf("unexpected error: %q", err)
	}

	done := make(chan struct{})
	<-done
}

type dispatcher struct{}

func (d *dispatcher) Echo(call ipc.ServerCall, arg string) (string, error) {
	return arg, nil
}
