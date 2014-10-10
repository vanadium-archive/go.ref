// Command tunneld is an implementation of the tunnel service.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"veyron.io/examples/tunnel"

	"veyron.io/veyron/veyron/lib/signals"
	sflag "veyron.io/veyron/veyron/security/flag"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on. For example, set to 'veyron' and set --address to the endpoint/name of a proxy to have this tunnel service proxied.")
	address  = flag.String("address", ":0", "address to listen on")
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
	r := rt.Init()
	defer r.Cleanup()
	auth := sflag.NewAuthorizerOrDie()
	server, err := r.NewServer(veyron2.DebugAuthorizerOpt{auth})
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	ep, err := server.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatalf("Listen(%q, %q) failed: %v", *protocol, *address, err)
	}
	vlog.Infof("Listening on endpoint %s", ep)
	hwaddr, err := firstHardwareAddrInUse()
	if err != nil {
		vlog.Fatalf("Couldn't find a good hw address: %v", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("os.Hostname failed: %v", err)
	}
	// TODO(rthellend): This is not secure. We should use
	// rt.R().Product().ID() and the associated verification, when it is
	// ready.
	names := []string{
		fmt.Sprintf("tunnel/hostname/%s", hostname),
		fmt.Sprintf("tunnel/hwaddr/%s", hwaddr),
		fmt.Sprintf("tunnel/id/%s", rt.R().Identity().PublicID()),
	}
	dispatcher := ipc.LeafDispatcher(tunnel.NewServerTunnel(&T{}), auth)
	published := false
	for _, n := range names {
		if err := server.Serve(n, dispatcher); err != nil {
			vlog.Infof("Serve(%v) failed: %v", n, err)
			continue
		}
		published = true
	}
	if !published {
		vlog.Fatalf("Failed to publish with any of %v", names)
	}
	vlog.Infof("Published as %v", names)
	<-signals.ShutdownOnSignals()
}
