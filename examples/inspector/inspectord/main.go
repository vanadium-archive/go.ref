package main

import (
	"fmt"
	"os"

	"veyron2"
	"veyron2/ipc"
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
	server, err := r.NewServer()
	if err != nil {
		log.Fatalf("failed to create server: %q", err)
	}
	for stubbed, prefix := range map[bool]string{true: "stubbed/", false: "stubless/"} {
		servers := []struct {
			name string
			disp ipc.Dispatcher
		}{
			{"files", NewFileSvc(stubbed)},
			{"proc", NewProcSvc(stubbed)},
			{"devices", NewDeviceSvc(stubbed)},
		}
		for _, s := range servers {
			if err := server.Register(prefix+s.name, s.disp); err != nil {
				log.Fatalf("failed to register %q: %q\n", s.name, err)
			}
		}
	}
	ep, err := server.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("listen failed: %q", err)
	}
	hostname, _ := os.Hostname()
	if err := server.Publish(hostname); err != nil {
		log.Fatalf("publish of %q failed: %q", hostname, err)
	}
	fmt.Printf("%s\n", ep)

	// Wait forever.
	done := make(chan struct{})
	<-done
}
