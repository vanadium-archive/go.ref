// Binary vsyncd is the sync daemon.
package main

import (
	"flag"

	"veyron/runtimes/google/vsync"
	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/vlog"
)

func main() {
	peerEndpoints := flag.String("peers", "",
		"comma separated list of endpoints of the vsync peer")
	peerDeviceIDs := flag.String("peerids", "",
		"comma separated list of deviceids of the vsync peer")
	devid := flag.String("devid", "", "Device ID")
	storePath := flag.String("store", "/tmp/", "path to store files")
	vstoreEndpoint := flag.String("vstore", "", "endpoint of the local Veyron store")
	address := flag.String("address", ":0", "address to listen on")

	flag.Parse()
	if *devid == "" {
		vlog.Fatalf("syncd:: --devid needs to be specified")
	}

	// Create the runtime.
	r := rt.Init()
	defer r.Shutdown()

	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		vlog.Fatalf("syncd:: failure creating server: err %v", err)
	}

	// Register the "sync" prefix with the sync dispatcher.
	syncd := vsync.NewSyncd(*peerEndpoints, *peerDeviceIDs, *devid, *storePath, *vstoreEndpoint)
	serverSync := vsync.NewServerSync(syncd)
	if err := s.Register("sync", ipc.SoloDispatcher(serverSync, nil)); err != nil {
		vlog.Fatalf("syncd:: error registering service: err %v", err)
	}

	// Create an endpoint and begin listening.
	if endpoint, err := s.Listen("tcp", *address); err == nil {
		vlog.VI(0).Infof("syncd:: Listening now at %v", endpoint)
	} else {
		vlog.Fatalf("syncd:: error listening to service: err %v", err)
	}

	// Publish the vsync service. This will register it in the mount table and maintain the
	// registration while the program runs.
	if err := s.Publish("sync"); err != nil {
		vlog.Fatalf("syncd: error publishing service: err %v", err)
	}

	// Wait forever.
	done := make(chan struct{})
	<-done
}
