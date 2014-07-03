// Binary vsyncd is the sync daemon.
package main

import (
	"flag"
	"os"

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
	storePath := flag.String("store", os.TempDir(), "path to store files")
	vstoreEndpoint := flag.String("vstore", "", "endpoint of the local Veyron store")
	// TODO(rthellend): Remove the address flag when the config manager is working.
	address := flag.String("address", ":0", "address to listen on")
	syncTick := flag.Duration("synctick", 0, "clock tick duration for sync with a peer (e.g. 10s)")

	flag.Parse()
	if *devid == "" {
		vlog.Fatalf("syncd:: --devid needs to be specified")
	}

	// Create the runtime.
	r := rt.Init()
	defer r.Cleanup()

	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		vlog.Fatalf("syncd:: failure creating server: err %v", err)
	}

	// Register the "sync" prefix with the sync dispatcher.
	syncd := vsync.NewSyncd(*peerEndpoints, *peerDeviceIDs, *devid, *storePath, *vstoreEndpoint, *syncTick)
	serverSync := vsync.NewServerSync(syncd)
	dispatcher := ipc.SoloDispatcher(serverSync, nil)

	// Create an endpoint and begin listening.
	if endpoint, err := s.Listen("tcp", *address); err == nil {
		vlog.VI(0).Infof("syncd:: Listening now at %v", endpoint)
	} else {
		vlog.Fatalf("syncd:: error listening to service: err %v", err)
	}

	// Publish the vsync service. This will register it in the mount table and maintain the
	// registration while the program runs.
	if err := s.Serve("sync", dispatcher); err != nil {
		vlog.Fatalf("syncd: error publishing service: err %v", err)
	}

	// Wait forever.
	done := make(chan struct{})
	<-done
}
