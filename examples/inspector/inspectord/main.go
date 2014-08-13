package main

import (
	"flag"
	"fmt"
	"os"

	"veyron2"
	"veyron2/rt"
	"veyron2/vlog"
)

var (
	log vlog.Logger
	r   veyron2.Runtime

	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")
)

func main() {
	r = rt.Init()
	log = r.Logger()
	hostname, _ := os.Hostname()
	server, err := r.NewServer()
	if err != nil {
		log.Fatalf("failed to create server: %q", err)
	}
	ep, err := server.Listen(*protocol, *address)
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
