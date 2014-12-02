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

// sortByProtocols sorts the supplied slice of serverEndpoints into a hash
// map keyed by the protocol of each of those endpoints where that protocol
// is listed in the protocols parameter and keyed by '*' if not so listed.
func sortByProtocol(eps []*serverEndpoint, protocols []string) (bool, map[string][]*serverEndpoint) {
	byProtocol := make(map[string][]*serverEndpoint)
	matched := false
	for _, ep := range eps {
		found := false
		for _, p := range protocols {
			if ep.iep.Protocol == p || (p == "tcp" && strings.HasPrefix(ep.iep.Protocol, "tcp")) {
				byProtocol[p] = append(byProtocol[p], ep)
				found = true
				matched = true
				break
			}
		}
		if !found {
			byProtocol["*"] = append(byProtocol["*"], ep)
		}
	}
	return matched, byProtocol
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

var defaultPreferredProtocolOrder = []string{"unixfd", "tcp4", "tcp", "*"}

// filterAndOrderServers returns a set of servers that are compatible with
// the current client in order of 'preference' specified by the supplied
// protocols and a notion of 'locality' according to the supplied protocol
// list as follows:
// - if the protocol parameter is non-empty, then only servers matching those
// protocols are returned and the endpoints are ordered first by protocol
// and then by locality within each protocol. If tcp4 and unixfd are requested
// for example then only protocols that match tcp4 and unixfd will returned
// with the tcp4 ones preceeding the unixfd ones.
// - if the protocol parameter is empty, then a default protocol ordering
// will be used, but unlike the previous case, any servers that don't support
// these protocols will be returned also, but following the default
// preferences.
func filterAndOrderServers(servers []string, protocols []string) ([]string, error) {
	errs := newErrorAccumulator()
	vlog.VI(3).Infof("Candidates[%v]: %v", protocols, servers)

	// TODO(cnicolaou): ideally we should filter out unsupported protocols
	// here - e.g. no point dialing on a ws protocol if it's not registered
	// etc.
	compatible := filterCompatibleEndpoints(errs, servers)
	if len(compatible) == 0 {
		return nil, fmt.Errorf("failed to find any compatible servers: %s", errs)
	}
	vlog.VI(3).Infof("Version Compatible: %v", compatible)

	defaultOrdering := len(protocols) == 0
	preferredProtocolOrder := defaultPreferredProtocolOrder
	if !defaultOrdering {
		preferredProtocolOrder = protocols
	}

	// put the server endpoints into per-protocol lists
	matched, byProtocol := sortByProtocol(compatible, preferredProtocolOrder)
	if !defaultOrdering && !matched {
		return nil, fmt.Errorf("failed to find any servers compatible with %v from %s", protocols, servers)
	}

	vlog.VI(3).Infof("Have Protocols(%v): %v", protocols, byProtocol)

	networks, err := netstate.GetAll()
	if err != nil {
		// return whatever we have now, just not sorted by locality.
		return sliceByProtocol(byProtocol, preferredProtocolOrder), nil
	}

	ordered := make([]*serverEndpoint, 0, len(byProtocol))
	for _, protocol := range preferredProtocolOrder {
		o := orderByLocality(networks, byProtocol[protocol])
		vlog.VI(3).Infof("Protocol: %q ordered by locality: %v", protocol, o)
		ordered = append(ordered, o...)
	}

	vlog.VI(2).Infof("Ordered By Locality: %v", ordered)
	return slice(ordered), nil
}
