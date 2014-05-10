// Binary vsyncd is the sync daemon.
package main

import (
	"flag"
	"log"

	"veyron/runtimes/google/vsync"
	"veyron2/ipc"
	"veyron2/rt"
)

func main() {
	peerEndpoints := flag.String("peers", "",
		"comma separated list of endpoints of the vsync peer")
	peerDeviceIDs := flag.String("peerids", "",
		"comma separated list of deviceids of the vsync peer")
	devid := flag.String("devid", "", "Device ID")
	storePath := flag.String("store", "/tmp/", "Path to store files")
	vstoreEndpoint := flag.String("vstore", "", "endpoint of the local Veyron store")

	flag.Parse()
	if *devid == "" {
		log.Fatal("syncd:: --devid needs to be specified")
	}

	// Create the runtime.
	r := rt.Init()
	defer r.Shutdown()

	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("syncd:: failure creating server: ", err)
	}

	// Register the "sync" prefix with the sync dispatcher.
	syncd := vsync.NewSyncd(*peerEndpoints, *peerDeviceIDs, *devid, *storePath, *vstoreEndpoint)
	serverSync := vsync.NewServerSync(syncd)
	if err := s.Register("sync", ipc.SoloDispatcher(serverSync, nil)); err != nil {
		log.Fatal("syncd:: error registering service: ", err)
	}

	// Create an endpoint and begin listening.
	if endpoint, err := s.Listen("tcp", ":0"); err == nil {
		log.Printf("syncd:: Listening now at %v\n", endpoint)
	} else {
		log.Fatal("syncd:: error listening to service: ", err)
	}

	// Publish the vsync service. This will register it in the mount table and maintain the
	// registration while the program runs.
	if err := s.Publish("sync"); err != nil {
		log.Fatal("syncd: error publishing service: ", err)
	}

	// Wait forever.
	done := make(chan struct{})
	<-done
}
