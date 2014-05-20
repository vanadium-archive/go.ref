// +build linux

// TODO(srdjan): port this to mac etc.
// Binary proximityd provides means for devices to announce their precence
// to nearby devices and to obtain the list of nearby devices.
package main

import (
	"flag"
	"time"

	"veyron/lib/signals"

	"veyron/runtimes/google/lib/bluetooth"
	"veyron/runtimes/google/lib/proximity"
	vflag "veyron/security/flag"
	"veyron2/ipc"
	"veyron2/rt"
	prox "veyron2/services/proximity"
	"veyron2/vlog"
)

var (
	protocol = flag.String("protocol", "tcp", "network to listen on. For example, set to 'veyron' and set --address to the endpoint/name of a proxy to have this service proxied.")
	address  = flag.String("address", ":0", "address to listen on")
	name     = flag.String("name", "", "name to mount the proximity service as")
)

func main() {
	// Get the runtime.
	r := rt.Init()
	defer r.Shutdown()

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
	scanner, err := bluetooth.OpenFirstAvailableDevice()
	if err != nil {
		vlog.Fatal("couldn't find an available bluetooth device")
	}
	defer scanner.Close()
	p, err := proximity.New(advertiser, scanner, 1*time.Second, 500*time.Millisecond)
	if err != nil {
		vlog.Fatal("couldn't create proximity service:", err)
	}
	defer p.Stop()

	// Register the service with the server.
	if err := s.Register("", ipc.SoloDispatcher(prox.NewServerProximity(p), vflag.NewAuthorizerOrDie())); err != nil {
		vlog.Fatal("error registering service:", err)
	}

	// Create an endpoint to listen on.
	endpoint, err := s.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatal("error listening:", err)
	}
	vlog.Info("Endpoint: ", endpoint)

	// Start the server and register it with the mounttable under the
	// given name.
	if err := s.Publish(*name); err != nil {
		vlog.Fatalf("error publishing service (%s): %v", *name, err)
	}

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}
