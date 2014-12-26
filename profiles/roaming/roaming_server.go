// +build ignore

package main

import (
	"fmt"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/profiles/roaming"
)

func main() {
	r, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %s", err)
	}
	defer r.Cleanup()
	log := r.Logger()

	server, err := r.NewServer()
	defer server.Stop()
	if err != nil {
		log.Fatalf("unexpected error: %q", err)
	}

	fmt.Printf("listen spec: %v\n", roaming.ListenSpec)
	ep, err := server.Listen(roaming.ListenSpec)
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

func (d *dispatcher) Echo(call ipc.ServerContext, arg string) (string, error) {
	return arg, nil
}
