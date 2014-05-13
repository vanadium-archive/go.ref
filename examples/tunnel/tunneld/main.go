package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"veyron/examples/tunnel"
	"veyron/examples/tunnel/tunneld/impl"
	"veyron/lib/signals"
	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/vlog"
)

var (
	protocol = flag.String("protocol", "tcp", "network to listen on. For example, set to 'veyron' and set --address to the endpoint/name of a proxy to have this tunnel service proxied.")
	address  = flag.String("address", ":0", "address to listen on")

	users = flag.String("users", "", "A comma-separated list of principal patterns allowed to use this service.")
)

// firstHardwareAddrInUse returns the hwaddr of the first network interface
// that is up, excluding loopback.
func firstHardwareAddrInUse() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, i := range interfaces {
		if i.Name != "lo" && i.Flags&net.FlagUp != 0 {
			name := i.HardwareAddr.String()
			vlog.Infof("Using %q (from %v)", name, i.Name)
			return name, nil
		}
	}
	return "", errors.New("No usable network interfaces")
}

func authorizer() security.Authorizer {
	ACL := make(security.ACL)
	principals := strings.Split(*users, ",")
	for _, p := range principals {
		ACL[security.PrincipalPattern(p)] = security.LabelSet(security.AdminLabel)
	}
	return security.NewACLAuthorizer(ACL)
}

func main() {
	r := rt.Init()
	defer r.Shutdown()
	server, err := r.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	if err := server.Register("", ipc.SoloDispatcher(tunnel.NewServerTunnel(&impl.T{}), authorizer())); err != nil {
		vlog.Fatalf("Register failed: %v", err)
	}
	ep, err := server.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatalf("Listen(%q, %q) failed: %v", "tcp", *address, err)
	}
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
	published := false
	for _, n := range names {
		if err := server.Publish(n); err != nil {
			vlog.Infof("Publish(%v) failed: %v", n, err)
			continue
		}
		published = true
	}
	if !published {
		vlog.Fatalf("Failed to publish with any of %v", names)
	}
	vlog.Infof("Listening on endpoint /%s (published as %v)", ep, names)
	<-signals.ShutdownOnSignals()
}
