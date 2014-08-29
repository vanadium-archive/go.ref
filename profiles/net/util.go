package net

import (
	"fmt"
	"net"

	"veyron/lib/netstate"
)

// TODO(cnicolaou): this will be removed in a subsequent CL by using the
// new netstate library.

// ipAndIf is a map of interface name to the set of IP addresses available on
// that interface.
type ipAndIf map[string][]net.IP

func (ifcs ipAndIf) String() string {
	r := ""
	for k, v := range ifcs {
		r += fmt.Sprintf("(%v: %v)", k, v)
	}
	return r
}

// empty returns true if there are no addresses on any interfaces.
func (ifcs ipAndIf) empty() bool {
	for _, addrs := range ifcs {
		if len(addrs) > 0 {
			return false
		}
	}
	return true
}

func (ifcs ipAndIf) first() (string, net.IP) {
	for ifc, addrs := range ifcs {
		if len(addrs) > 0 {
			return ifc, addrs[0]
		}
	}
	return "", nil
}

func (ifcs ipAndIf) has(ip net.IP) bool {
	for _, addrs := range ifcs {
		for _, a := range addrs {
			if a.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// ipState returns the set of IPv4 and IPv6 addresses available to the running
// process.
func ipState() (ip4, ip6 ipAndIf, err error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}
	ip4 = make(ipAndIf)
	ip6 = make(ipAndIf)
	for _, ifc := range interfaces {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipn, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if t := ip.To4(); t != nil {
				ip4[ifc.Name] = append(ip4[ifc.Name], ip)
			}
			if t := ip.To16(); t != nil {
				ip6[ifc.Name] = append(ip6[ifc.Name], ip)
			}
		}
	}
	return ip4, ip6, nil
}

func filterPublic(ips ipAndIf) ipAndIf {
	public := make(ipAndIf)
	for ifc, addrs := range ips {
		for _, a := range addrs {
			if publicIP(a) {
				public[ifc] = append(public[ifc], a)
			}
		}
	}
	return public
}

// publicIPState returns the set of IPv4 and IPv6 addresses available
// to the running process that are publicly/globally routable.
func publicIPState() (ipAndIf, ipAndIf, error) {
	v4, v6, err := ipState()
	if err != nil {
		return nil, nil, err
	}
	return filterPublic(v4), filterPublic(v6), nil
}

// publicIP returns true if the supplied IP address is publicly/gobally
// routable.
func publicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return netstate.IsGloballyRoutableIP(ip)
}

func diffAB(a, b ipAndIf) ipAndIf {
	diff := make(ipAndIf)
	for ak, av := range a {
		if b[ak] == nil {
			diff[ak] = av
			continue
		}
		for _, v := range av {
			found := false
			for _, bv := range b[ak] {
				if v.Equal(bv) {
					found = true
					break
				}
			}
			if !found {
				diff[ak] = append(diff[ak], v)
			}
		}
	}
	return diff
}

// findAdded returns the set of interfaces and/or addresses that are
// present in b, but not in a - i.e. have been added.
func findAdded(a, b ipAndIf) ipAndIf {
	return diffAB(b, a)
}

// findRemoved returns the set of interfaces and/or addresses that
// are present in a, but not in b - i.e. have been removed.
func findRemoved(a, b ipAndIf) ipAndIf {
	return diffAB(a, b)
}
