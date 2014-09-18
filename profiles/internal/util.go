package internal

import (
	"fmt"
	"net"

	"veyron.io/veyron/veyron/lib/netstate"
)

// IPAddressChooser returns the preferred IP address, which is,
// a public IPv4 address, then any non-loopback IPv4, then a public
// IPv6 address and finally any non-loopback/link-local IPv6
func IPAddressChooser(network string, addrs []net.Addr) (net.Addr, error) {
	if !netstate.IsIPProtocol(network) {
		return nil, fmt.Errorf("can't support network protocol %q", network)
	}
	al := netstate.AddrList(addrs).Map(netstate.ConvertToIPHost)
	for _, predicate := range []netstate.Predicate{netstate.IsPublicUnicastIPv4,
		netstate.IsUnicastIPv4, netstate.IsPublicUnicastIPv6} {
		if a := al.First(predicate); a != nil {
			return a, nil
		}
	}
	return nil, fmt.Errorf("failed to find any usable address for %q", network)
}
