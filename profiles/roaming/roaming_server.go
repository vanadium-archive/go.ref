// +build ignore

package main

import (
	"fmt"
	"log"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/vlog"

	_ "v.io/core/veyron/profiles/roaming"
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

	_, err = server.Listen(listenSpec)
	if err != nil {
		vlog.Fatalf("unexpected error: %q", err)
	}
	err = server.Serve("roamer", &dummy{}, nil)
	if err != nil {
		log.Fatalf("unexpected error: %q", err)
	}
	watcher := make(chan ipc.NetworkChange, 1)
	server.WatchNetwork(watcher)

	for {
		status := server.Status()
		fmt.Printf("Endpoints: %d created:\n", len(status.Endpoints))
		for _, ep := range status.Endpoints {
			fmt.Printf("  %s\n", ep)
		}
		fmt.Printf("Mounts: %d mounts:\n", len(status.Mounts))
		for _, ms := range status.Mounts {
			fmt.Printf("  %s\n", ms)
		}
		change := <-watcher
		fmt.Printf("Network change: %s", change.DebugString())
	}
}

type dummy struct{}

func (d *dummy) Echo(call ipc.ServerContext, arg string) (string, error) {
	return arg, nil
}
