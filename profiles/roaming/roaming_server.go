// +build ignore

package main

import (
	"fmt"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/profiles/roaming"
)

func main() {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	server, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("unexpected error: %q", err)
	}

	listenSpec := veyron2.GetListenSpec(ctx)

	fmt.Printf("listen spec: %v\n", listenSpec)
	ep, err := server.Listen(listenSpec)
	if err != nil {
		vlog.Fatalf("unexpected error: %q", err)
	}
	if ep != nil {
		fmt.Println(ep)
	}
	if err := server.Serve("roamer", &receiver{}, nil); err != nil {
		vlog.Fatalf("unexpected error: %q", err)
	}

	done := make(chan struct{})
	<-done
}

type receiver struct{}

func (d *receiver) Echo(call ipc.ServerContext, arg string) (string, error) {
	return arg, nil
}
