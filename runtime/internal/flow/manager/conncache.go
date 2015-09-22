// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"sort"
	"strings"
	"sync"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/x/ref/runtime/internal/flow/conn"
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
}

type connEntry struct {
	conn    *conn.Conn
	rid     naming.RoutingID
	addrKey string
}

func NewConnCache() *ConnCache {
	mu := &sync.Mutex{}
	cond := sync.NewCond(mu)
	return &ConnCache{
		mu:        mu,
		addrCache: make(map[string]*connEntry),
		ridCache:  make(map[naming.RoutingID]*connEntry),
		started:   make(map[string]bool),
		cond:      cond,
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
func (c *ConnCache) ReservedFind(protocol, address string, blessingNames []string) (*conn.Conn, error) {
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
		return nil, nil
	}
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
func (c *ConnCache) Insert(conn *conn.Conn) error {
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
	return nil
}

// InsertWithRoutingID add conn to the cache keyed only by conn's RoutingID.
func (c *ConnCache) InsertWithRoutingID(conn *conn.Conn) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(nil)
	}
	entry := &connEntry{
		conn: conn,
		rid:  conn.RemoteEndpoint().RoutingID(),
	}
	c.ridCache[entry.rid] = entry
	return nil
}

// Close marks the ConnCache as closed and closes all Conns in the cache.
func (c *ConnCache) Close(ctx *context.T) {
	defer c.mu.Unlock()
	c.mu.Lock()
	c.addrCache, c.started = nil, nil
	for _, d := range c.ridCache {
		d.conn.Close(ctx, NewErrCacheClosed(ctx))
	}
}

// KillConnections will close and remove num LRU Conns in the cache.
// If connections are already closed they will be removed from the cache.
// This is useful when the manager is approaching system FD limits.
// If num is greater than the number of connections in the cache, all cached
// connections will be closed and removed.
// KillConnections returns an error iff the cache is closed.
func (c *ConnCache) KillConnections(ctx *context.T, num int) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(ctx)
	}
	err := NewErrConnKilledToFreeResources(ctx)
	pq := make(connEntries, 0, len(c.ridCache))
	for _, e := range c.ridCache {
		if isClosed(e.conn) {
			delete(c.addrCache, e.addrKey)
			delete(c.ridCache, e.rid)
			continue
		}
		if e.conn.IsEncapsulated() {
			// Killing a proxied connection doesn't save us any FD resources, just memory.
			continue
		}
		pq = append(pq, e)
	}
	sort.Sort(pq)
	for i := 0; i < num; i++ {
		d := pq[i]
		d.conn.Close(ctx, err)
		delete(c.addrCache, d.addrKey)
		delete(c.ridCache, d.rid)
	}
	return nil
}

// TODO(suharshs): If sorting the connections becomes too slow, switch to
// container/heap instead of sorting all the connections.
type connEntries []*connEntry

func (c connEntries) Len() int {
	return len(c)
}

func (c connEntries) Less(i, j int) bool {
	return c[i].conn.LastUsedTime().Before(c[j].conn.LastUsedTime())
}

func (c connEntries) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

// FindWithRoutingID returns a Conn where the remote end of the connection
// is identified by the provided rid. nil is returned if there is no such Conn.
// FindWithRoutingID will return an error iff the cache is closed.
func (c *ConnCache) FindWithRoutingID(rid naming.RoutingID) (*conn.Conn, error) {
	if rid == naming.NullRoutingID {
		return nil, nil
	}
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
		return nil, nil
	}
	return entry.conn, nil
}

func key(protocol, address string, blessingNames []string) string {
	// TODO(suharshs): We may be able to do something more inclusive with our
	// blessingNames.
	return strings.Join(append([]string{protocol, address}, blessingNames...), ",")
}

func isClosed(conn *conn.Conn) bool {
	select {
	case <-conn.Closed():
		return true
	default:
		return false
	}
}
