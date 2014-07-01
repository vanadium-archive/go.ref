package main

import (
	"fmt"
	"os"

	"veyron2"
	"veyron2/rt"
	"veyron2/vlog"
)

var (
	log vlog.Logger
	r   veyron2.Runtime
)

func main() {
	var err error
	r = rt.Init()
	log = r.Logger()
	if err != nil {
		log.Fatalf("failed to init runtime: %q", err)
	}
	hostname, _ := os.Hostname()
	server, err := r.NewServer()
	if err != nil {
		log.Fatalf("failed to create server: %q", err)
	}
	ep, err := server.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("listen failed: %q", err)
	}
	if err := server.Serve(hostname, &dispatcher{}); err != nil {
		log.Fatalf("failed to register %q: %q\n", hostname, err)
	}
	defer server.Stop()
	fmt.Printf("%s\n", ep)
	// Wait forever.
	done := make(chan struct{})
	<-done
}
