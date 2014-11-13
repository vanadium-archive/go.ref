package ipc

import (
	"fmt"
	"net"
	"strings"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/netstate"
	"veyron.io/veyron/veyron/runtimes/google/ipc/version"
	inaming "veyron.io/veyron/veyron/runtimes/google/naming"
)

type errorAccumulator struct {
	errs []error
}

func (e *errorAccumulator) add(err error) {
	e.errs = append(e.errs, err)
}

func (e *errorAccumulator) failed() bool {
	return len(e.errs) > 0
}

func (e *errorAccumulator) String() string {
	r := ""
	for _, err := range e.errs {
		r += fmt.Sprintf("(%s)", err)
	}
	return r
}

// TODO(cnicolaou): simplify this code, especially the use of maps+slices
// and special cases.

func newErrorAccumulator() *errorAccumulator {
	return &errorAccumulator{errs: make([]error, 0, 4)}
}

type serverEndpoint struct {
	iep    *inaming.Endpoint
	suffix string
}

func (se *serverEndpoint) String() string {
	return fmt.Sprintf("(%s, %q)", se.iep, se.suffix)
}

func filterCompatibleEndpoints(errs *errorAccumulator, servers []string) []*serverEndpoint {
	se := make([]*serverEndpoint, 0, len(servers))
	for _, server := range servers {
		name := server
		address, suffix := naming.SplitAddressName(name)
		if len(address) == 0 {
			errs.add(fmt.Errorf("%q is not a rooted name", name))
			continue
		}
		iep, err := inaming.NewEndpoint(address)
		if err != nil {
			errs.add(fmt.Errorf("%q: %s", name, err))
			continue
		}
		if err = version.CheckCompatibility(iep); err != nil {
			errs.add(fmt.Errorf("%q: %s", name, err))
			continue
		}
		sep := &serverEndpoint{iep, suffix}
		se = append(se, sep)
	}
	return se
}

func sortByProtocol(eps []*serverEndpoint) map[string][]*serverEndpoint {
	byProtocol := make(map[string][]*serverEndpoint)
	for _, ep := range eps {
		p := ep.iep.Protocol
		byProtocol[p] = append(byProtocol[p], ep)
	}
	return byProtocol
}

func unmatchedProtocols(hashed map[string][]*serverEndpoint, protocols []string) []*serverEndpoint {
	unmatched := make([]*serverEndpoint, 0, 10)
	for p, eps := range hashed {
		found := false
		for _, preferred := range protocols {
			if p == preferred {
				found = true
				break
			}
		}
		if !found {
			unmatched = append(unmatched, eps...)
		}
	}
	return unmatched
}

func orderByLocality(ifcs netstate.AddrList, eps []*serverEndpoint) []*serverEndpoint {
	if len(ifcs) <= 1 {
		return append([]*serverEndpoint{}, eps...)
	}
	ipnets := make([]*net.IPNet, 0, len(ifcs))
	for _, a := range ifcs {
		// Try IP
		_, ipnet, err := net.ParseCIDR(a.Address().String())
		if err != nil {
			continue
		}
		ipnets = append(ipnets, ipnet)
	}
	if len(ipnets) == 0 {
		return eps
	}
	// TODO(cnicolaou): this can obviously be made more efficient...
	local := make([]*serverEndpoint, 0, len(eps))
	remote := make([]*serverEndpoint, 0, len(eps))
	notip := make([]*serverEndpoint, 0, len(eps))
	for _, ep := range eps {
		if strings.HasPrefix(ep.iep.Protocol, "tcp") || strings.HasPrefix(ep.iep.Protocol, "ws") {
			// Take care to use the Address directly, since the network
			// may be marked as a 'websocket'. This throws out any thought
			// of dealing with IPv6 etc and web sockets.
			host, _, err := net.SplitHostPort(ep.iep.Address)
			if err != nil {
				host = ep.iep.Address
			}
			ip := net.ParseIP(host)
			if ip == nil {
				notip = append(notip, ep)
				continue
			}
			found := false
			for _, ipnet := range ipnets {
				if ipnet.Contains(ip) {
					local = append(local, ep)
					found = true
					break
				}
			}
			if !found {
				remote = append(remote, ep)
			}
		} else {
			notip = append(notip, ep)
		}
	}
	return append(local, append(remote, notip...)...)
}

func slice(eps []*serverEndpoint) []string {
	r := make([]string, len(eps))
	for i, a := range eps {
		r[i] = naming.JoinAddressName(a.iep.String(), a.suffix)
	}
	return r
}

func sliceByProtocol(eps map[string][]*serverEndpoint, protocols []string) []string {
	r := make([]string, 0, 10)
	for _, p := range protocols {
		r = append(r, slice(eps[p])...)
	}
	return r
}

var defaultPreferredProtocolOrder = []string{"unixfd", "tcp4", "tcp", "tcp6"}

func filterAndOrderServers(servers []string, protocols []string) ([]string, error) {
	errs := newErrorAccumulator()
	vlog.VI(3).Infof("Candidates[%v]: %v", protocols, servers)
	compatible := filterCompatibleEndpoints(errs, servers)
	if len(compatible) == 0 {
		return nil, fmt.Errorf("failed to find any compatible servers: %s", errs)
	}
	vlog.VI(3).Infof("Version Compatible: %v", compatible)

	// put the server endpoints into per-protocol lists
	byProtocol := sortByProtocol(compatible)

	if len(protocols) > 0 {
		found := 0
		for _, p := range protocols {
			found += len(byProtocol[p])
		}
		if found == 0 {
			return nil, fmt.Errorf("failed to find any servers compatible with %v from %s", protocols, servers)
		}
	}

	// If a set of protocols is specified, then we will order
	// and return endpoints that contain those protocols alone.
	// However, if no protocols are supplied we'll order by
	// a default ordering but append any endpoints that don't belong
	// to that default ordering set to the returned endpoints.
	remaining := []*serverEndpoint{}
	preferredProtocolOrder := defaultPreferredProtocolOrder
	if len(protocols) > 0 {
		preferredProtocolOrder = protocols
	} else {
		remaining = unmatchedProtocols(byProtocol, preferredProtocolOrder)
	}

	vlog.VI(3).Infof("Have Protocols(%v): %v", protocols, byProtocol)

	networks, err := netstate.GetAll()
	if err != nil {
		r := sliceByProtocol(byProtocol, preferredProtocolOrder)
		r = append(r, slice(remaining)...)
		return r, nil
	}

	ordered := make([]*serverEndpoint, 0, len(byProtocol))
	for _, protocol := range preferredProtocolOrder {
		o := orderByLocality(networks, byProtocol[protocol])
		ordered = append(ordered, o...)
	}

	if len(protocols) == 0 {
		ordered = append(ordered, remaining...)
	}
	vlog.VI(2).Infof("Ordered By Locality: %v", ordered)
	return slice(ordered), nil
}
