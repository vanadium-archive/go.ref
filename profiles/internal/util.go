package internal

import (
	"fmt"
	"os"
	"strings"

	"v.io/v23/ipc"
	"v.io/x/lib/vlog"

	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/exec"
	"v.io/x/ref/lib/flags"
)

// ParseFlags parses all registered flags taking into account overrides from other
// configuration and environment variables. It must be called by the profile and
// flags.RuntimeFlags() must be passed to the runtime initialization function. The
// profile can use or modify the flags as it pleases.
func ParseFlags(f *flags.Flags) error {
	handle, err := exec.GetChildHandle()
	switch err {
	case exec.ErrNoVersion:
		// The process has not been started through the vanadium exec
		// library. No further action is needed.
	case nil:
		// The process has been started through the vanadium exec
		// library.
	default:
		return err
	}

	// Parse runtime flags.
	var config map[string]string
	if handle != nil {
		config = handle.Config.Dump()
	}
	f.Parse(os.Args[1:], config)
	return nil
}

// IPAddressChooser returns the preferred IP address, which is,
// a public IPv4 address, then any non-loopback IPv4, then a public
// IPv6 address and finally any non-loopback/link-local IPv6
func IPAddressChooser(network string, addrs []ipc.Address) ([]ipc.Address, error) {
	if !netstate.IsIPProtocol(network) {
		return nil, fmt.Errorf("can't support network protocol %q", network)
	}
	accessible := netstate.AddrList(addrs)

	// Try and find an address on a interface with a default route.
	// We give preference to IPv4 over IPv6 for compatibility for now.
	var predicates []netstate.AddressPredicate
	if !strings.HasSuffix(network, "6") {
		predicates = append(predicates, netstate.IsPublicUnicastIPv4, netstate.IsUnicastIPv4)
	}
	if !strings.HasSuffix(network, "4") {
		predicates = append(predicates, netstate.IsPublicUnicastIPv6, netstate.IsUnicastIPv6)
	}
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
