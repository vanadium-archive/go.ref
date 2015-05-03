// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
	mu      sync.RWMutex
	set     map[string][]*VIF // GUARDED_BY(mu)
	started map[string]bool   // GUARDED_BY(mu)
	cond    *sync.Cond
}

// NewSet returns a new Set of VIFs.
func NewSet() *Set {
	s := &Set{
		set:     make(map[string][]*VIF),
		started: make(map[string]bool),
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// BlockingFind returns a VIF where the remote end of the underlying network connection
// is identified by the provided (network, address). Returns nil if there is no
// such VIF.
//
// If BlockingFind returns nil, the caller is required to call Unblock, to avoid deadlock.
// The network and address in Unblock must be the same as used in the BlockingFind call.
// During this time, all new BlockingFind calls for this network and address will Block until
// the corresponding Unblock call is made.
func (s *Set) BlockingFind(network, address string) *VIF {
	return s.find(network, address, true)
}

// Unblock marks the status of the network, address as no longer started, and
// broadcasts waiting threads.
func (s *Set) Unblock(network, address string) {
	s.mu.Lock()
	delete(s.started, key(network, address))
	s.cond.Broadcast()
	s.mu.Unlock()
}

// Find returns a VIF where the remote end of the underlying network connection
// is identified by the provided (network, address). Returns nil if there is no
// such VIF.
func (s *Set) Find(network, address string) *VIF {
	return s.find(network, address, false)
}

// Insert adds a VIF to the set.
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

// Delete removes a VIF from the set.
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

func (s *Set) find(network, address string, blocking bool) *VIF {
	if isNonDistinctConn(network, address) {
		return nil
	}

	k := key(network, address)

	s.mu.Lock()
	defer s.mu.Unlock()

	for blocking && s.started[k] {
		s.cond.Wait()
	}

	_, _, p := rpc.RegisteredProtocol(network)
	for _, n := range p {
		if vifs := s.set[key(n, address)]; len(vifs) > 0 {
			return vifs[rand.Intn(len(vifs))]
		}
	}

	if blocking {
		s.started[k] = true
	}
	return nil
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

// Some network connections (like those created with net.Pipe or Unix sockets)
// do not end up with distinct net.Addrs on distinct net.Conns.
func isNonDistinctConn(network, address string) bool {
	return len(address) == 0 ||
		(network == "pipe" && address == "pipe") ||
		(runtime.GOOS == "linux" && network == "unix" && address == "@")
}
