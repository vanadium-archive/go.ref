package internal

import (
	"fmt"

	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/vlog"

	"v.io/veyron/veyron/lib/netstate"
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

// HasPublicIP returns true if the host has at least one public IP address.
func HasPublicIP(log vlog.Logger) bool {
	state, err := netstate.GetAccessibleIPs()
	if err != nil {
		log.Infof("failed to determine network state: %s", err)
		return false
	}
	any := state.Filter(netstate.IsUnicastIP)
	if len(any) == 0 {
		log.Infof("failed to find any usable IP addresses at startup")
		return false
	}
	for _, a := range any {
		if netstate.IsPublicUnicastIPv4(a) {
			return true
		}
	}
	return false
}
