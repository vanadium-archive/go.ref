// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"fmt"
	"net"
	"os"
	"strings"

	"v.io/v23/logging"
	"v.io/v23/verror"
	"v.io/x/lib/netstate"
	"v.io/x/ref/internal/logger"
	"v.io/x/ref/lib/exec"
	"v.io/x/ref/lib/flags"
)

func legacyExec() (exec.Config, error) {
	handle, err := exec.GetChildHandle()
	if err == nil {
		return handle.Config, nil
	} else if verror.ErrorID(err) == exec.ErrNoVersion.ID {
		// Do not initialize the mgmt runtime if the process has not
		// been started through the vanadium exec library by a device
		// manager.
		return nil, nil
	}
	return nil, err
}

// ParseFlags parses all registered flags taking into account overrides from other
// configuration and environment variables. It must be called by the profile and
// flags.RuntimeFlags() must be passed to the runtime initialization function. The
// profile can use or modify the flags as it pleases.
func ParseFlags(f *flags.Flags) error {
	config, err := exec.ReadConfigFromOSEnv()
	if config == nil && err == nil {
		// TODO(cnicolaou): backwards compatibility, remove when binaries are pushed to prod.
		config, err = legacyExec()
	}
	if err != nil {
		return err
	}

	// Parse runtime flags.
	var args map[string]string
	if config != nil {
		args = config.Dump()
	}
	return f.Parse(os.Args[1:], args)
}

// ParseFlagsAndConfigurGlobalLogger calls ParseFlags and then
// ConfigureGlobalLoggerFromFlags.
func ParseFlagsAndConfigureGlobalLogger(f *flags.Flags) error {
	if err := ParseFlags(f); err != nil {
		return err
	}
	if err := ConfigureGlobalLoggerFromFlags(); err != nil {
		return err
	}
	return nil
}

// ConfigureGlobalLoggerFromFlags configures the global logger from command
// line flags.  Should be called immediately after ParseFlags.
func ConfigureGlobalLoggerFromFlags() error {
	err := logger.Manager(logger.Global()).ConfigureFromFlags()
	if err != nil && !logger.IsAlreadyConfiguredError(err) {
		return err
	}
	return nil
}

// IPAddressChooser returns the preferred IP address, which is,
// a public IPv4 address, then any non-loopback IPv4, then a public
// IPv6 address and finally any non-loopback/link-local IPv6
type IPAddressChooser struct{}

func (IPAddressChooser) ChooseAddresses(network string, addrs []net.Addr) ([]net.Addr, error) {
	if !netstate.IsIPProtocol(network) {
		return nil, fmt.Errorf("can't support network protocol %q", network)
	}
	accessible := netstate.ConvertToAddresses(addrs)

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
				return onDefaultRoutes.AsNetAddrs(), nil
			}
		}
	}

	// We failed to find any addresses with default routes, try again
	// but without the default route requirement.
	for _, predicate := range predicates {
		if addrs := accessible.Filter(predicate); len(addrs) > 0 {
			return addrs.AsNetAddrs(), nil
		}
	}
	return []net.Addr{}, nil
}

// HasPublicIP returns true if the host has at least one public IP address.
func HasPublicIP(log logging.Logger) bool {
	state, err := netstate.GetAccessibleIPs()
	if err != nil {
		log.VI(0).Infof("failed to determine network state: %s", err)
		return false
	}
	any := state.Filter(netstate.IsUnicastIP)
	if len(any) == 0 {
		log.VI(1).Infof("failed to find any usable IP addresses at startup")
		return false
	}
	for _, a := range any {
		if netstate.IsPublicUnicastIPv4(a) {
			return true
		}
	}
	return false
}
