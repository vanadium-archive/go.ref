// Binary vsyncd is the sync daemon.
package main

import (
	"flag"
	"os"

	"veyron/runtimes/google/vsync"
	vflag "veyron/security/flag"

	"veyron2/rt"
	"veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

	peerEndpoints = flag.String("peers", "",
		"comma separated list of endpoints of the vsync peer")
	peerDeviceIDs = flag.String("peerids", "",
		"comma separated list of deviceids of the vsync peer")
	devid          = flag.String("devid", "", "Device ID")
	storePath      = flag.String("store", os.TempDir(), "path to store files")
	vstoreEndpoint = flag.String("vstore", "", "endpoint of the local Veyron store")
	syncTick       = flag.Duration("synctick", 0, "clock tick duration for sync with a peer (e.g. 10s)")
)

func main() {
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

	// Create a new SyncService.
	syncd := vsync.NewSyncd(*peerEndpoints, *peerDeviceIDs, *devid, *storePath, *vstoreEndpoint, *syncTick)
	syncService := vsync.NewServerSync(syncd)

	// Create the authorizer.
	auth := vflag.NewAuthorizerOrDie()

	// Register the service.
	syncDisp := vsync.NewSyncDispatcher(syncService, auth)

	// Create an endpoint and begin listening.
	if endpoint, err := s.Listen(*protocol, *address); err == nil {
		vlog.VI(0).Infof("syncd:: Listening now at %v", endpoint)
	} else {
		vlog.Fatalf("syncd:: error listening to service: err %v", err)
	}

	// Publish the vsync service.
	name := "global/vsync/" + *devid
	if err := s.Serve(name, syncDisp); err != nil {
		vlog.Fatalf("syncd: error publishing service: err %v", err)
	}

	// Wait forever.
	done := make(chan struct{})
	<-done
}
