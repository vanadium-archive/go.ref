// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"strings"
	"sync"

	"v.io/v23/naming"
)

// ConnCache is a cache of Conns keyed by (protocol, address, blessingNames)
// and (routingID).
// Multiple goroutines can invoke methods on the ConnCache simultaneously.
// TODO(suharshs): We should periodically look for closed connections and remove them.
type ConnCache struct {
	mu        *sync.Mutex
	addrCache map[string]*connEntry           // keyed by (protocol, address, blessingNames)
	ridCache  map[naming.RoutingID]*connEntry // keyed by naming.RoutingID
	started   map[string]bool                 // keyed by (protocol, address, blessingNames)
	cond      *sync.Cond
	head      *connEntry // the head and tail pointer of the linked list for implementing LRU.
}

type connEntry struct {
	conn       *Conn
	rid        naming.RoutingID
	addrKey    string
	next, prev *connEntry
}

func NewConnCache() *ConnCache {
	mu := &sync.Mutex{}
	cond := sync.NewCond(mu)
	head := &connEntry{}
	head.next, head.prev = head, head
	return &ConnCache{
		mu:        mu,
		addrCache: make(map[string]*connEntry),
		ridCache:  make(map[naming.RoutingID]*connEntry),
		started:   make(map[string]bool),
		cond:      cond,
		head:      head,
	}
}

// ReservedFind returns a Conn where the remote end of the connection is
// identified by the provided (protocol, address, blessingNames). nil is
// returned if there is no such Conn.
//
// ReservedFind will return an error iff the cache is closed.
// If no error is returned, the caller is required to call Unreserve, to avoid
// deadlock. The (protocol, address, blessingNames) provided to Unreserve must
// be the same as the arguments provided to ReservedFind.
// All new ReservedFind calls for the (protocol, address, blessings) will Block
// until the corresponding Unreserve call is made.
func (c *ConnCache) ReservedFind(protocol, address string, blessingNames []string) (*Conn, error) {
	k := key(protocol, address, blessingNames)
	defer c.mu.Unlock()
	c.mu.Lock()
	for c.started[k] {
		c.cond.Wait()
	}
	if c.addrCache == nil {
		return nil, NewErrCacheClosed(nil)
	}
	c.started[k] = true
	entry := c.addrCache[k]
	if entry == nil {
		return nil, nil
	}
	if isClosed(entry.conn) {
		delete(c.addrCache, entry.addrKey)
		delete(c.ridCache, entry.rid)
		entry.removeFromList()
		return nil, nil
	}
	entry.moveAfter(c.head)
	return entry.conn, nil
}

// Unreserve marks the status of the (protocol, address, blessingNames) as no
// longer started, and broadcasts waiting threads.
func (c *ConnCache) Unreserve(protocol, address string, blessingNames []string) {
	c.mu.Lock()
	delete(c.started, key(protocol, address, blessingNames))
	c.cond.Broadcast()
	c.mu.Unlock()
}

// Insert adds conn to the cache.
// An error will be returned iff the cache has been closed.
func (c *ConnCache) Insert(conn *Conn) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(nil)
	}
	ep := conn.RemoteEndpoint()
	k := key(ep.Addr().Network(), ep.Addr().String(), ep.BlessingNames())
	entry := &connEntry{
		conn:    conn,
		rid:     ep.RoutingID(),
		addrKey: k,
	}
	c.addrCache[k] = entry
	c.ridCache[entry.rid] = entry
	entry.moveAfter(c.head)
	return nil
}

// Close marks the ConnCache as closed and closes all Conns in the cache.
func (c *ConnCache) Close() {
	defer c.mu.Unlock()
	c.mu.Lock()
	c.addrCache, c.ridCache, c.started = nil, nil, nil
	d := c.head.next
	for d != c.head {
		d.conn.Close()
		d = d.next
	}
	c.head = nil
}

// KillConnections will close and remove num LRU Conns in the cache.
// This is useful when the manager is approaching system FD limits.
// If num is greater than the number of connections in the cache, all cached
// connections will be closed and removed.
// KillConnections returns an error iff the cache is closed.
func (c *ConnCache) KillConnections(num int) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(nil)
	}
	d := c.head.prev
	for i := 0; i < num; i++ {
		if d == c.head {
			break
		}
		d.conn.Close()
		delete(c.addrCache, d.addrKey)
		delete(c.ridCache, d.rid)
		prev := d.prev
		d.removeFromList()
		d = prev
	}
	return nil
}

// FindWithRoutingID returns a Conn where the remote end of the connection
// is identified by the provided rid. nil is returned if there is no such Conn.
// FindWithRoutingID will return an error iff the cache is closed.
func (c *ConnCache) FindWithRoutingID(rid naming.RoutingID) (*Conn, error) {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return nil, NewErrCacheClosed(nil)
	}
	entry := c.ridCache[rid]
	if entry == nil {
		return nil, nil
	}
	if isClosed(entry.conn) {
		delete(c.addrCache, entry.addrKey)
		delete(c.ridCache, entry.rid)
		entry.removeFromList()
		return nil, nil
	}
	entry.moveAfter(c.head)
	return entry.conn, nil
}

// Size returns the number of Conns stored in the ConnCache.
func (c *ConnCache) Size() int {
	defer c.mu.Unlock()
	c.mu.Lock()
	return len(c.addrCache)
}

func key(protocol, address string, blessingNames []string) string {
	// TODO(suharshs): We may be able to do something more inclusive with our
	// blessingNames.
	return strings.Join(append([]string{protocol, address}, blessingNames...), ",")
}

func (c *connEntry) removeFromList() {
	if c.prev != nil {
		c.prev.next = c.next
	}
	if c.next != nil {
		c.next.prev = c.prev
	}
	c.next, c.prev = nil, nil
}

func (c *connEntry) moveAfter(prev *connEntry) {
	c.removeFromList()
	c.prev = prev
	c.next = prev.next
	prev.next.prev = c
	prev.next = c
}

func isClosed(conn *Conn) bool {
	select {
	case <-conn.closed:
		return true
	default:
		return false
	}
}
