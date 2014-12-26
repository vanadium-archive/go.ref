// Command tunneld is an implementation of the tunnel service.
package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/profiles/roaming"
	sflag "v.io/core/veyron/security/flag"

	"v.io/apps/tunnel"
)

// firstHardwareAddrInUse returns the hwaddr of the first network interface
// that is up, excluding loopback.
func firstHardwareAddrInUse() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, i := range interfaces {
		if !strings.HasPrefix(i.Name, "lo") && i.Flags&net.FlagUp != 0 {
			name := i.HardwareAddr.String()
			if len(name) == 0 {
				continue
			}
			vlog.Infof("Using %q (from %v)", name, i.Name)
			return name, nil
		}
	}
	return "", errors.New("No usable network interfaces")
}

func main() {
	r, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime %v", err)
	}
	defer r.Cleanup()

	auth := sflag.NewAuthorizerOrDie()
	server, err := r.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	eps, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%v) failed: %v", roaming.ListenSpec, err)
	}
	vlog.Infof("Listening on: %v", eps)
	hwaddr, err := firstHardwareAddrInUse()
	if err != nil {
		vlog.Fatalf("Couldn't find a good hw address: %v", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("os.Hostname failed: %v", err)
	}
	names := []string{
		fmt.Sprintf("tunnel/hostname/%s", hostname),
		fmt.Sprintf("tunnel/hwaddr/%s", hwaddr),
	}
	published := false
	if err := server.Serve(names[0], tunnel.TunnelServer(&T{}), auth); err != nil {
		vlog.Infof("Serve(%v) failed: %v", names[0], err)
	}
	published = true
	for _, n := range names[1:] {
		server.AddName(n)
	}
	if !published {
		vlog.Fatalf("Failed to publish with any of %v", names)
	}
	vlog.Infof("Published as %v", names)
	<-signals.ShutdownOnSignals(r)
}
