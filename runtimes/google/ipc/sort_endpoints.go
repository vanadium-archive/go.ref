package ipc

import (
	"fmt"
	"net"
	"sort"

	"v.io/v23/naming"
	"v.io/x/lib/vlog"

	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/runtimes/google/ipc/version"
	inaming "v.io/core/veyron/runtimes/google/naming"
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

func newErrorAccumulator() *errorAccumulator {
	return &errorAccumulator{errs: make([]error, 0, 4)}
}

type serverLocality int

const (
	unknownNetwork serverLocality = iota
	remoteNetwork
	localNetwork
)

type sortableServer struct {
	server       naming.MountedServer
	protocolRank int            // larger values are preferred.
	locality     serverLocality // larger values are preferred.
}

func (s *sortableServer) String() string {
	return fmt.Sprintf("%v", s.server)
}

type sortableServerList []sortableServer

func (l sortableServerList) Len() int      { return len(l) }
func (l sortableServerList) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
func (l sortableServerList) Less(i, j int) bool {
	if l[i].protocolRank == l[j].protocolRank {
		return l[i].locality > l[j].locality
	}
	return l[i].protocolRank > l[j].protocolRank
}

func mkProtocolRankMap(list []string) map[string]int {
	if len(list) == 0 {
		return nil
	}
	m := make(map[string]int)
	for idx, protocol := range list {
		m[protocol] = len(list) - idx
	}
	return m
}

var defaultPreferredProtocolOrder = mkProtocolRankMap([]string{"unixfd", "wsh", "tcp4", "tcp", "*"})

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
func filterAndOrderServers(servers []naming.MountedServer, protocols []string, ipnets []*net.IPNet) ([]naming.MountedServer, error) {
	vlog.VI(3).Infof("filterAndOrderServers%v: %v", protocols, servers)
	var (
		errs       = newErrorAccumulator()
		list       = make(sortableServerList, 0, len(servers))
		protoRanks = mkProtocolRankMap(protocols)
	)
	if len(protoRanks) == 0 {
		protoRanks = defaultPreferredProtocolOrder
	}
	for _, server := range servers {
		name := server.Server
		ep, err := name2endpoint(name)
		if err != nil {
			errs.add(fmt.Errorf("malformed endpoint %q: %v", name, err))
			continue
		}
		if err = version.CheckCompatibility(ep); err != nil {
			errs.add(fmt.Errorf("%q: %v", name, err))
			continue
		}
		rank, err := protocol2rank(ep.Addr().Network(), protoRanks)
		if err != nil {
			errs.add(fmt.Errorf("%q: %v", name, err))
			continue
		}
		list = append(list, sortableServer{
			server:       server,
			protocolRank: rank,
			locality:     locality(ep, ipnets),
		})
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("failed to find any compatible servers: %v", errs)
	}
	// TODO(ashankar): Don't have to use stable sorting, could
	// just use sort.Sort. The only problem with that is the
	// unittest.
	sort.Stable(list)
	// Convert to []naming.MountedServer
	ret := make([]naming.MountedServer, len(list))
	for idx, item := range list {
		ret[idx] = item.server
	}
	return ret, nil
}

// name2endpoint returns the naming.Endpoint encoded in a name.
func name2endpoint(name string) (naming.Endpoint, error) {
	addr := name
	if naming.Rooted(name) {
		addr, _ = naming.SplitAddressName(name)
	}
	return inaming.NewEndpoint(addr)
}

// protocol2rank returns the "rank" of a protocol (given a map of ranks).
// The higher the rank, the more preferable the protocol.
func protocol2rank(protocol string, ranks map[string]int) (int, error) {
	if r, ok := ranks[protocol]; ok {
		return r, nil
	}
	// Special case: if "wsh" has a rank but "wsh4"/"wsh6" don't,
	// then they get the same rank as "wsh". Similar for "tcp" and "ws".
	//
	// TODO(jhahn): We have similar protocol equivalency checks at a few places.
	// Figure out a way for this mapping to be shared.
	if p := protocol; p == "wsh4" || p == "wsh6" || p == "tcp4" || p == "tcp6" || p == "ws4" || p == "ws6" {
		if r, ok := ranks[p[:len(p)-1]]; ok {
			return r, nil
		}
	}
	// "*" means that any protocol is acceptable.
	if r, ok := ranks["*"]; ok {
		return r, nil
	}
	// UnknownProtocol should be rare, it typically happens when
	// the endpoint is described in <host>:<port> format instead of
	// the full fidelity description (@<version>@<protocol>@...).
	if protocol == naming.UnknownProtocol {
		return -1, nil
	}
	return 0, fmt.Errorf("undesired protocol %q", protocol)
}

// locality returns the serverLocality to use given an endpoint and the
// set of IP networks configured on this machine.
func locality(ep naming.Endpoint, ipnets []*net.IPNet) serverLocality {
	if len(ipnets) < 1 {
		return unknownNetwork // 0 IP networks, locality doesn't matter.

	}
	host, _, err := net.SplitHostPort(ep.Addr().String())
	if err != nil {
		host = ep.Addr().String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Not an IP address (possibly not an IP network).
		return unknownNetwork
	}
	for _, ipnet := range ipnets {
		if ipnet.Contains(ip) {
			return localNetwork
		}
	}
	return remoteNetwork
}

// ipNetworks returns the IP networks on this machine.
func ipNetworks() []*net.IPNet {
	ifcs, err := netstate.GetAll()
	if err != nil {
		vlog.VI(5).Infof("netstate.GetAll failed: %v", err)
		return nil
	}
	ret := make([]*net.IPNet, 0, len(ifcs))
	for _, a := range ifcs {
		_, ipnet, err := net.ParseCIDR(a.Address().String())
		if err != nil {
			vlog.VI(5).Infof("net.ParseCIDR(%q) failed: %v", a.Address(), err)
			continue
		}
		ret = append(ret, ipnet)
	}
	return ret
}
