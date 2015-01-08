package mounttable

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"v.io/core/veyron/lib/glob"
	"v.io/core/veyron/lib/netconfig"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mounttable"
	"v.io/core/veyron2/services/security/access"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	mdns "github.com/presotto/go-mdns-sd"
)

const addressPrefix = "address:"

// neighborhood defines a set of machines on the same multicast media.
type neighborhood struct {
	mdns   *mdns.MDNS
	nelems int
	nw     netconfig.NetConfigWatcher
}

var _ ipc.Dispatcher = (*neighborhood)(nil)

type neighborhoodService struct {
	name  string
	elems []string
	nh    *neighborhood
}

func getPort(address string) uint16 {
	epAddr, _ := naming.SplitAddressName(address)

	ep, err := veyron2.NewEndpoint(epAddr)
	if err != nil {
		return 0
	}
	addr := ep.Addr()
	if addr == nil {
		return 0
	}
	if addr.Network() != "tcp" {
		return 0
	}
	_, pstr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0
	}
	port, err := strconv.ParseUint(pstr, 10, 16)
	if err != nil || port == 0 {
		return 0
	}
	return uint16(port)
}

func newNeighborhoodServer(host string, addresses []string, loopback bool) (*neighborhood, error) {
	// Create the TXT contents with addresses to announce. Also pick up a port number.
	var txt []string
	var port uint16
	for _, addr := range addresses {
		txt = append(txt, addressPrefix+addr)
		if port == 0 {
			port = getPort(addr)
		}
	}
	if txt == nil {
		return nil, errors.New("neighborhood passed no useful addresses")
	}
	if port == 0 {
		return nil, errors.New("neighborhood couldn't determine a port to use")
	}

	// Start up MDNS, subscribe to the veyron service, and add us as a veyron service provider.
	mdns, err := mdns.NewMDNS(host, "", "", loopback, false)
	if err != nil {
		vlog.Errorf("mdns startup failed: %s", err)
		return nil, err
	}
	vlog.VI(2).Infof("listening for service veyron on port %d", port)
	mdns.SubscribeToService("veyron")
	mdns.AddService("veyron", "", port, txt...)

	nh := &neighborhood{
		mdns: mdns,
	}

	// Watch the network configuration so that we can make MDNS reattach to
	// interfaces when the network changes.
	nh.nw, err = netconfig.NewNetConfigWatcher()
	if err != nil {
		vlog.Errorf("nighborhood can't watch network: %s", err)
		return nh, nil
	}
	go func() {
		if _, ok := <-nh.nw.Channel(); !ok {
			return
		}
		if _, err := nh.mdns.ScanInterfaces(); err != nil {
			vlog.Errorf("nighborhood can't scan interfaces: %s", err)
		}
	}()

	return nh, nil
}

// NewLoopbackNeighborhoodServer creates a new instance of a neighborhood server on loopback interfaces for testing.
func NewLoopbackNeighborhoodServer(host string, addresses ...string) (*neighborhood, error) {
	return newNeighborhoodServer(host, addresses, true)
}

// NewNeighborhoodServer creates a new instance of a neighborhood server.
func NewNeighborhoodServer(host string, addresses ...string) (*neighborhood, error) {
	return newNeighborhoodServer(host, addresses, false)
}

// Lookup implements ipc.Dispatcher.Lookup.
func (nh *neighborhood) Lookup(name string) (interface{}, security.Authorizer, error) {
	vlog.VI(1).Infof("*********************LookupServer '%s'\n", name)
	elems := strings.Split(name, "/")[nh.nelems:]
	if name == "" {
		elems = nil
	}
	ns := &neighborhoodService{
		name:  name,
		elems: elems,
		nh:    nh,
	}
	return mounttable.MountTableServer(ns), nh, nil
}

func (nh *neighborhood) Authorize(context security.Context) error {
	// TODO(rthellend): Figure out whether it's OK to accept all requests
	// unconditionally.
	return nil
}

// Stop performs cleanup.
func (nh *neighborhood) Stop() {
	if nh.nw != nil {
		nh.nw.Stop()
	}
	nh.mdns.Stop()
}

// neighbor returns the MountedServers for a particular neighbor.
func (nh *neighborhood) neighbor(instance string) []naming.VDLMountedServer {
	var reply []naming.VDLMountedServer
	si := nh.mdns.ResolveInstance(instance, "veyron")

	// Use a map to dedup any addresses seen
	addrMap := make(map[string]uint32)

	// Look for any TXT records with addresses.
	for _, rr := range si.TxtRRs {
		for _, s := range rr.Txt {
			if !strings.HasPrefix(s, addressPrefix) {
				continue
			}
			addr := s[len(addressPrefix):]
			addrMap[addr] = rr.Header().Ttl
		}
	}
	for addr, ttl := range addrMap {
		reply = append(reply, naming.VDLMountedServer{addr, ttl})
	}

	if reply != nil {
		return reply
	}

	// If we didn't get any direct endpoints, make some up from the target and port.
	// TODO(rthellend): Do we need the code below? If we haven't received
	// any results at this point, it would seem to indicate a bug somewhere
	// because NeighborhoodServer won't start without at least one address.
	for _, rr := range si.SrvRRs {
		ips, ttl := nh.mdns.ResolveAddress(rr.Target)
		for _, ip := range ips {
			addr := net.JoinHostPort(ip.String(), strconv.Itoa(int(rr.Port)))
			ep := naming.FormatEndpoint("tcp", addr)
			reply = append(reply, naming.VDLMountedServer{naming.JoinAddressName(ep, ""), ttl})
		}
	}
	return reply
}

// neighbors returns all neighbors and their MountedServer structs.
func (nh *neighborhood) neighbors() map[string][]naming.VDLMountedServer {
	neighbors := make(map[string][]naming.VDLMountedServer, 0)
	members := nh.mdns.ServiceDiscovery("veyron")
	for _, m := range members {
		if neighbor := nh.neighbor(m.Name); neighbor != nil {
			neighbors[m.Name] = neighbor
		}
	}
	vlog.VI(2).Infof("members %v neighbors %v", members, neighbors)
	return neighbors
}

// ResolveStep implements ResolveStep
func (ns *neighborhoodService) ResolveStep(ctx ipc.ServerContext) (servers []naming.VDLMountedServer, suffix string, err error) {
	nh := ns.nh
	vlog.VI(2).Infof("ResolveStep %v\n", ns.elems)
	if len(ns.elems) == 0 {
		//nothing can be mounted at the root
		return nil, "", verror.Make(naming.ErrNoSuchNameRoot, ctx.Context(), ns.elems)
	}

	// We can only resolve the first element and it always refers to a mount table (for now).
	neighbor := nh.neighbor(ns.elems[0])
	if neighbor == nil {
		return nil, "", verror.Make(naming.ErrNoSuchName, ctx.Context(), ns.elems)
	}
	return neighbor, naming.Join(ns.elems[1:]...), nil
}

// ResolveStepX implements ResolveStepX
func (ns *neighborhoodService) ResolveStepX(ctx ipc.ServerContext) (entry naming.VDLMountEntry, err error) {
	nh := ns.nh
	vlog.VI(2).Infof("ResolveStep %v\n", ns.elems)
	if len(ns.elems) == 0 {
		//nothing can be mounted at the root
		err = verror.Make(naming.ErrNoSuchNameRoot, ctx.Context(), ns.elems)
		return
	}

	// We can only resolve the first element and it always refers to a mount table (for now).
	neighbor := nh.neighbor(ns.elems[0])
	if neighbor == nil {
		err = verror.Make(naming.ErrNoSuchName, ctx.Context(), ns.elems)
		entry.Name = ns.name
		return
	}
	entry.MT = true
	entry.Name = naming.Join(ns.elems[1:]...)
	entry.Servers = neighbor
	return
}

// Mount not implemented.
func (*neighborhoodService) Mount(_ ipc.ServerContext, server string, ttlsecs uint32, opts naming.MountFlag) error {
	return errors.New("this server does not implement Mount")
}

// Unmount not implemented.
func (*neighborhoodService) Unmount(_ ipc.ServerContext, _ string) error {
	return errors.New("this server does not implement Unmount")
}

// Delete not implemented.
func (*neighborhoodService) Delete(_ ipc.ServerContext, _ bool) error {
	return errors.New("this server does not implement Delete")
}

// Glob__ implements ipc.AllGlobber
func (ns *neighborhoodService) Glob__(ctx ipc.ServerContext, pattern string) (<-chan naming.VDLMountEntry, error) {
	g, err := glob.Parse(pattern)
	if err != nil {
		return nil, err
	}

	// return all neighbors that match the first element of the pattern.
	nh := ns.nh

	switch len(ns.elems) {
	case 0:
		ch := make(chan naming.VDLMountEntry)
		go func() {
			defer close(ch)
			for k, n := range nh.neighbors() {
				if ok, _, _ := g.MatchInitialSegment(k); !ok {
					continue
				}
				ch <- naming.VDLMountEntry{Name: k, Servers: n, MT: true}
			}
		}()
		return ch, nil
	case 1:
		neighbor := nh.neighbor(ns.elems[0])
		if neighbor == nil {
			return nil, verror.Make(naming.ErrNoSuchName, ctx.Context(), ns.elems[0])
		}
		ch := make(chan naming.VDLMountEntry, 1)
		ch <- naming.VDLMountEntry{Name: "", Servers: neighbor, MT: true}
		close(ch)
		return ch, nil
	default:
		return nil, verror.Make(naming.ErrNoSuchName, ctx.Context(), ns.elems)
	}
}

func (*neighborhoodService) SetACL(ctx ipc.ServerContext, acl access.TaggedACLMap, etag string) error {
	return errors.New("this server does not implement SetACL")
}

func (*neighborhoodService) GetACL(ctx ipc.ServerContext) (acl access.TaggedACLMap, etag string, err error) {
	return nil, "", nil
}
