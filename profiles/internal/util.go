package internal

import (
	"fmt"

	"veyron.io/veyron/veyron2/ipc"

	"veyron.io/veyron/veyron/lib/netstate"
)

// IPAddressChooser returns the preferred IP address, which is,
// a public IPv4 address, then any non-loopback IPv4, then a public
// IPv6 address and finally any non-loopback/link-local IPv6
func IPAddressChooser(network string, addrs []ipc.Address) ([]ipc.Address, error) {
	if !netstate.IsIPProtocol(network) {
		return nil, fmt.Errorf("can't support network protocol %q", network)
	}
	accessible := netstate.AddrList(addrs)

	// Try and find an address on a interface with a default route.
	predicates := []netstate.AddressPredicate{netstate.IsPublicUnicastIPv4,
		netstate.IsUnicastIPv4, netstate.IsPublicUnicastIPv6}
	for _, predicate := range predicates {
		if addrs := accessible.Filter(predicate); len(addrs) > 0 {
			onDefaultRoutes := addrs.Filter(netstate.IsOnDefaultRoute)
			if len(onDefaultRoutes) > 0 {
				return onDefaultRoutes, nil
			}
		}
	}

	// We failed to find any addresses with default routes, try again
	// but without the default route requirement.
	for _, predicate := range predicates {
		if addrs := accessible.Filter(predicate); len(addrs) > 0 {
			return addrs, nil
		}
	}

	return nil, fmt.Errorf("failed to find any usable address for %q", network)
}
