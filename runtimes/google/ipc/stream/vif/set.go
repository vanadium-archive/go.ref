package vif

import (
	"math/rand"
	"runtime"
	"sync"
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
		// Some network connections (like those created with net.Pipe
		// or Unix sockets) do not end up with distinct conn.RemoteAddrs
		// on distinct net.Conns. For those cases, avoid the cache collisions
		// by disabling cache lookups for them.
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.set[s.key(network, address)]
	if !ok || len(l) == 0 {
		return nil
	}
	return l[rand.Intn(len(l))]
}

// Insert adds a VIF to the set
func (s *Set) Insert(vif *VIF) {
	addr := vif.conn.RemoteAddr()
	key := s.key(addr.Network(), addr.String())
	s.mu.Lock()
	defer s.mu.Unlock()
	l, _ := s.set[key]
	for _, v := range l {
		if v == vif {
			return
		}
	}
	s.set[key] = append(l, vif)
	vif.addSet(s)
}

// Delete removes a VIF from the set
func (s *Set) Delete(vif *VIF) {
	vif.removeSet(s)
	addr := vif.conn.RemoteAddr()
	key := s.key(addr.Network(), addr.String())
	s.mu.Lock()
	defer s.mu.Unlock()
	l, _ := s.set[key]
	for ix, v := range l {
		if v == vif {
			if len(l) == 1 {
				delete(s.set, key)
			} else {
				s.set[key] = append(l[:ix], l[ix+1:]...)
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

func (s *Set) key(network, address string) string {
	return network + ":" + address
}
