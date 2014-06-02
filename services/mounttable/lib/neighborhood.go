package mounttable

import (
	"errors"
	"net"
	"path"
	"strconv"
	"strings"

	"veyron/lib/glob"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mounttable"
	"veyron2/vlog"

	"code.google.com/p/mdns"
)

const endpointPrefix = "endpoint:"

// neighborhood defines a set of machines on the same multicast media.
type neighborhood struct {
	mdns   *mdns.MDNS
	prefix string // mount point in the process namespace
	nelems int
}

type neighborhoodService struct {
	name  string
	elems []string
	nh    *neighborhood
}

func getPort(e naming.Endpoint) uint16 {
	addr := e.Addr()
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

func newNeighborhoodServer(prefix, host string, eps []naming.Endpoint, loopback bool) (*neighborhood, error) {
	// Create the TXT contents with endpoints to announce.   Also pick up a port number.
	var txt []string
	var port uint16
	for _, ep := range eps {
		txt = append(txt, endpointPrefix+ep.String())
		if port == 0 {
			port = getPort(ep)
		}
	}
	if txt == nil {
		return nil, errors.New("neighborhood passed no useful endpoint")
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
		mdns:   mdns,
		prefix: prefix,
	}
	return nh, nil
}

// NewLoopbackNeighborhoodServer creates a new instance of a neighborhood server on loopback interfaces for testing.
func NewLoopbackNeighborhoodServer(prefix, host string, eps []naming.Endpoint) (*neighborhood, error) {
	return newNeighborhoodServer(prefix, host, eps, true)
}

// NewNeighborhoodServer creates a new instance of a neighborhood server.
func NewNeighborhoodServer(prefix, host string, eps []naming.Endpoint) (*neighborhood, error) {
	return newNeighborhoodServer(prefix, host, eps, false)
}

// Lookup implements ipc.Dispatcher.Lookup.
func (nh *neighborhood) Lookup(name string) (ipc.Invoker, security.Authorizer, error) {
	vlog.Infof("*********************LookupServer '%s'\n", name)
	elems := strings.Split(name, "/")[nh.nelems:]
	if name == "" {
		elems = nil
	}
	ns := &neighborhoodService{
		name:  name,
		elems: elems,
		nh:    nh,
	}
	return ipc.ReflectInvoker(mounttable.NewServerMountTable(ns)), nil, nil
}

// Stop performs cleanup.
func (nh *neighborhood) Stop() {
	nh.mdns.Stop()
}

// neighbor returns the MountedServers for a particular neighbor.
func (nh *neighborhood) neighbor(instance string) []mounttable.MountedServer {
	var reply []mounttable.MountedServer
	si := nh.mdns.ResolveInstance(instance, "veyron")

	// If we have any TXT records with endpoints, they take precedence.
	for _, rr := range si.TxtRRs {
		// Use a map to dedup any endpoints seen
		epmap := make(map[string]uint32)
		for _, s := range rr.Txt {
			if !strings.HasPrefix(s, endpointPrefix) {
				continue
			}
			epstring := s[len(endpointPrefix):]
			// Make sure its a legal endpoint string.
			if _, err := rt.R().NewEndpoint(epstring); err != nil {
				continue
			}
			epmap[epstring] = rr.Header().Ttl
		}
		for epstring, ttl := range epmap {
			reply = append(reply, mounttable.MountedServer{naming.JoinAddressName(epstring, ""), ttl})
		}
	}
	if reply != nil {
		return reply
	}

	// If we didn't get any direct endpoints, make some up from the target and port.
	for _, rr := range si.SrvRRs {
		ips, ttl := nh.mdns.ResolveAddress(rr.Target)
		for _, ip := range ips {
			addr := net.JoinHostPort(ip.String(), strconv.Itoa(int(rr.Port)))
			ep := naming.FormatEndpoint("tcp", addr)
			reply = append(reply, mounttable.MountedServer{naming.JoinAddressName(ep, ""), ttl})
		}
	}
	return reply
}

// neighbors returns all neighbors and their MountedServer structs.
func (nh *neighborhood) neighbors() map[string][]mounttable.MountedServer {
	neighbors := make(map[string][]mounttable.MountedServer, 0)
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
func (ns *neighborhoodService) ResolveStep(_ ipc.ServerContext) (servers []mounttable.MountedServer, suffix string, err error) {
	nh := ns.nh
	vlog.VI(2).Infof("ResolveStep %v\n", ns.elems)
	if len(ns.elems) == 0 {
		//nothing can be mounted at the root
		return nil, "", naming.ErrNoSuchNameRoot
	}

	// We can only resolve the first element and it always refers to a mount table (for now).
	neighbor := nh.neighbor(ns.elems[0])
	if neighbor == nil {
		return nil, "", naming.ErrNoSuchName
	}
	return neighbor, path.Join(ns.elems[1:]...), nil
}

// Mount not implemented.
func (*neighborhoodService) Mount(_ ipc.ServerContext, server string, ttlsecs uint32) error {
	return errors.New("this server does not implement Mount")
}

// Unmount not implemented.
func (*neighborhoodService) Unmount(_ ipc.ServerContext, _ string) error {
	return errors.New("this server does not implement Unmount")
}

// Glob implements Glob
func (ns *neighborhoodService) Glob(_ ipc.ServerContext, pattern string, reply mounttable.GlobableServiceGlobStream) error {
	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}

	// return all neighbors that match the first element of the pattern.
	nh := ns.nh

	switch len(ns.elems) {
	case 0:
		for k, n := range nh.neighbors() {
			if ok, _ := g.MatchInitialSegment(k); !ok {
				continue
			}
			if err := reply.Send(mounttable.MountEntry{Name: k, Servers: n}); err != nil {
				return err
			}
		}
		return nil
	case 1:
		neighbor := nh.neighbor(ns.elems[0])
		if neighbor == nil {
			return naming.ErrNoSuchName
		}
		return reply.Send(mounttable.MountEntry{Name: "", Servers: neighbor})
	default:
		return naming.ErrNoSuchName
	}
}
