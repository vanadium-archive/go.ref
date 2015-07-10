// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main

import (
	"fmt"

	"v.io/v23"
	"v.io/v23/rpc"
	"v.io/x/ref/lib/xrpc"
	_ "v.io/x/ref/runtime/factories/roaming"
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	server, err := xrpc.NewServer(ctx, "roamer", &dummy{}, nil)
	if err != nil {
		ctx.Fatalf("unexpected error: %q", err)
	}
	watcher := make(chan rpc.NetworkChange, 1)
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

func (d *dummy) Echo(call rpc.ServerCall, arg string) (string, error) {
	return arg, nil
}
