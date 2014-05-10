// Binary gatewayd provides TCP/IP connectivity to devices that don't have it
// by using their alternate connections (e.g., Bluetooth).  This binary
// should be started on both connected and non-connected devices and it will
// take an appropriate role depending on that device's configuration (i.e., it
// will take on either a connectivity-giving or a connectivity-taking role).
package main

import (
	"flag"
	"fmt"
	"os"

	"veyron/lib/signals"

	"veyron/runtimes/google/lib/gateway"
	"veyron2/rt"
	"veyron2/vlog"
)

// TODO(spetrovic): Once the mounttable is finalized, get this endpoint by
// traversing the device-wide mounttable, looking for the proximity service
// name.
var (
	proximity   = flag.String("proximity", "", "Endpoint string for the proximity service, running on this device")
	forceClient = flag.Bool("force_client", false, "Force this device to act as a client, for testing purposes")
)

func main() {
	// TODO(cnicolaou): rt.Init also calls flag.Parse and has its own
	// flags etc. We need to somehow expose those here so that a complete
	// usage description can be provided by apps that have their own flags.
	flag.Parse()
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s: gateway daemon that brings TCP/IP connectivity to devices using their alternate connections (e.g., Bluetooth).", os.Args[0])
		flag.PrintDefaults()
	}

	// Get the runtime.
	r := rt.Init()
	defer r.Shutdown()

	// Create a new instance of the gateway service.
	vlog.Info("Connecting to proximity service: ", *proximity)
	g, err := gateway.New(*proximity, *forceClient)
	if err != nil {
		vlog.Errorf("couldn't create gateway service: %v", err)
		return
	}
	defer g.Stop()

	// Block until the process gets terminated (either by AppMgr or system).
	<-signals.ShutdownOnSignals()
}
