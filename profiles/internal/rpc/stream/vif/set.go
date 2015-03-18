package vif

import (
	"math/rand"
	"net"
	"runtime"
	"sync"

	"v.io/v23/rpc"
)

// Set implements a set of VIFs keyed by (network, address) of the underlying
// connection.  Multiple goroutines can invoke methods on the Set
// simultaneously.
type Set struct {
	mu  sync.RWMutex
	set map[string][]*VIF
}

// NewSet returns a new Set of VIFs.
func NewSet() *Set {
	return &Set{set: make(map[string][]*VIF)}
}

// Find returns a VIF where the remote end of the underlying network connection
// is identified by the provided (network, address). Returns nil if there is no
// such VIF.
//
// If there are multiple VIFs established to the same remote network address,
// Find will randomly return one of them.
func (s *Set) Find(network, address string) *VIF {
	if len(address) == 0 ||
		(network == "pipe" && address == "pipe") ||
		(runtime.GOOS == "linux" && network == "unix" && address == "@") { // autobind
		// Some network connections (like those created with net.Pipe or Unix sockets)
		// do not end up with distinct net.Addrs on distinct net.Conns. For those cases,
		// avoid the cache collisions by disabling cache lookups for them.
		return nil
	}

	var keys []string
	_, _, p := rpc.RegisteredProtocol(network)
	for _, n := range p {
		keys = append(keys, key(n, address))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range keys {
		vifs := s.set[k]
		if len(vifs) > 0 {
			return vifs[rand.Intn(len(vifs))]
		}
	}
	return nil
}

// Insert adds a VIF to the set
func (s *Set) Insert(vif *VIF) {
	addr := vif.conn.RemoteAddr()
	k := key(addr.Network(), addr.String())
	s.mu.Lock()
	defer s.mu.Unlock()
	vifs := s.set[k]
	for _, v := range vifs {
		if v == vif {
			return
		}
	}
	s.set[k] = append(vifs, vif)
	vif.addSet(s)
}

// Delete removes a VIF from the set
func (s *Set) Delete(vif *VIF) {
	vif.removeSet(s)
	addr := vif.conn.RemoteAddr()
	k := key(addr.Network(), addr.String())
	s.mu.Lock()
	defer s.mu.Unlock()
	vifs := s.set[k]
	for i, v := range vifs {
		if v == vif {
			if len(vifs) == 1 {
				delete(s.set, k)
			} else {
				s.set[k] = append(vifs[:i], vifs[i+1:]...)
			}
			return
		}
	}
}

// List returns the elements in the set as a slice.
func (s *Set) List() []*VIF {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l := make([]*VIF, 0, len(s.set))
	for _, vifs := range s.set {
		l = append(l, vifs...)
	}
	return l
}

func key(network, address string) string {
	if network == "tcp" || network == "ws" {
		host, _, _ := net.SplitHostPort(address)
		switch ip := net.ParseIP(host); {
		case ip == nil:
			// This may happen when address is a hostname. But we do not care
			// about it, since vif cannot be found with a hostname anyway.
		case ip.To4() != nil:
			network += "4"
		default:
			network += "6"
		}
	}
	return network + ":" + address
}
