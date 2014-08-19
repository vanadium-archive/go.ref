// Binary proximityd provides means for devices to announce their precence
// to nearby devices and to obtain the list of nearby devices.
package main

import (
	"flag"
	"time"

	"veyron/lib/signals"

	"veyron/lib/bluetooth"
	vflag "veyron/security/flag"
	"veyron/services/proximity/lib"
	"veyron2/ipc"
	"veyron2/rt"
	prox "veyron2/services/proximity"
	"veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

	name = flag.String("name", "", "name to mount the proximity service as")
)

func main() {
	// Get the runtime.
	r := rt.Init()
	defer r.Cleanup()

	// Create a new server.
	s, err := r.NewServer()
	if err != nil {
		vlog.Fatal("error creating server: ", err)
	}
	defer s.Stop()

	// Create a new instance of the proximity service that uses bluetooth.
	// NOTE(spetrovic): the underlying Linux bluetooth library doesn't
	// allow us to scan and advertise on the same bluetooth device
	// descriptor.  We therefore open separate device descriptors for
	// advertising and scanning.
	advertiser, err := bluetooth.OpenFirstAvailableDevice()
	if err != nil {
		vlog.Fatal("couldn't find an available bluetooth device")
	}
	defer advertiser.Close()
	scanner, err := proximity.NewBluetoothScanner()
	if err != nil {
		vlog.Fatalf("couldn't create bluetooth scanner: %v", err)
	}
	p, err := proximity.New(advertiser, scanner, 1*time.Second, 500*time.Millisecond)
	if err != nil {
		vlog.Fatal("couldn't create proximity service:", err)
	}
	defer p.Stop()

	// Create an endpoint to listen on.
	endpoint, err := s.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatal("error listening:", err)
	}
	vlog.Info("Endpoint: ", endpoint)

	// Start the server and register it with the mounttable under the
	// given name.
	if err := s.Serve(*name, ipc.LeafDispatcher(prox.NewServerProximity(p), vflag.NewAuthorizerOrDie())); err != nil {
		vlog.Fatalf("error publishing service (%s): %v", *name, err)
	}

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
