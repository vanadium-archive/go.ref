// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"bytes"
	"fmt"
	"sort"
	"sync"

	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/security"
	iflow "v.io/x/ref/runtime/internal/flow"
	"v.io/x/ref/runtime/internal/flow/conn"
)

// ConnCache is a cache of Conns keyed by (protocol, address) and (routingID).
// Multiple goroutines can invoke methods on the ConnCache simultaneously.
type ConnCache struct {
	mu            *sync.Mutex
	cond          *sync.Cond
	addrCache     map[string]*connEntry           // keyed by (protocol, address)
	ridCache      map[naming.RoutingID]*connEntry // keyed by naming.RoutingID
	started       map[string]bool                 // keyed by (protocol, address)
	unmappedConns map[*connEntry]bool             // list of connEntries replaced by other entries
}

type connEntry struct {
	conn    *conn.Conn
	rid     naming.RoutingID
	addrKey string
	proxy   bool
}

func NewConnCache() *ConnCache {
	mu := &sync.Mutex{}
	cond := sync.NewCond(mu)
	return &ConnCache{
		mu:            mu,
		cond:          cond,
		addrCache:     make(map[string]*connEntry),
		ridCache:      make(map[naming.RoutingID]*connEntry),
		started:       make(map[string]bool),
		unmappedConns: make(map[*connEntry]bool),
	}
}

func (c *ConnCache) String() string {
	buf := &bytes.Buffer{}
	fmt.Fprintln(buf, "AddressCache:")
	for k, v := range c.addrCache {
		fmt.Fprintf(buf, "%v: %p\n", k, v.conn)
	}
	fmt.Fprintln(buf, "RIDCache:")
	for k, v := range c.ridCache {
		fmt.Fprintf(buf, "%v: %p\n", k, v.conn)
	}
	return buf.String()
}

// Insert adds conn to the cache, keyed by both (protocol, address) and (routingID)
// An error will be returned iff the cache has been closed.
func (c *ConnCache) Insert(conn *conn.Conn, proxy bool) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(nil)
	}
	ep := conn.RemoteEndpoint()
	addr := ep.Addr()
	k := key(addr.Network(), addr.String())
	entry := &connEntry{
		conn:    conn,
		rid:     ep.RoutingID(),
		addrKey: k,
		proxy:   proxy,
	}
	if old := c.ridCache[entry.rid]; old != nil {
		c.unmappedConns[old] = true
	}
	c.addrCache[k] = entry
	c.ridCache[entry.rid] = entry
	return nil
}

// InsertWithRoutingID adds conn to the cache keyed only by conn's RoutingID.
func (c *ConnCache) InsertWithRoutingID(conn *conn.Conn, proxy bool) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(nil)
	}
	ep := conn.RemoteEndpoint()
	entry := &connEntry{
		conn:  conn,
		rid:   ep.RoutingID(),
		proxy: proxy,
	}
	if old := c.ridCache[entry.rid]; old != nil {
		c.unmappedConns[old] = true
	}
	c.ridCache[entry.rid] = entry
	return nil
}

// Find returns a Conn based only on the RoutingID of remote.
func (c *ConnCache) FindWithRoutingID(ctx *context.T, remote naming.Endpoint,
	auth flow.PeerAuthorizer) (entry *conn.Conn, names []string, rejected []security.RejectedBlessing, err error) {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return nil, nil, nil, NewErrCacheClosed(nil)
	}
	if rid := remote.RoutingID(); rid != naming.NullRoutingID {
		if entry, names, rejected := c.removeUndialable(ctx, remote, c.ridCache[rid], auth); entry != nil {
			return entry, names, rejected, nil
		}
	}
	return nil, nil, nil, nil
}

// Find returns a Conn based on the input remoteEndpoint.
// nil is returned if there is no such Conn.
//
// Find will return an error iff the cache is closed.
// If no error is returned, the caller is required to call Unreserve, to avoid
// deadlock. The (protocol, address) provided to Unreserve must be the same as
// the arguments provided to Find.
// All new Find calls for the (protocol, address) will Block
// until the corresponding Unreserve call is made.
// p is used to check the cache for resolved protocols.
func (c *ConnCache) Find(ctx *context.T, remote naming.Endpoint, network, address string, auth flow.PeerAuthorizer,
	p flow.Protocol) (entry *conn.Conn, names []string, rejected []security.RejectedBlessing, err error) {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return nil, nil, nil, NewErrCacheClosed(nil)
	}
	if rid := remote.RoutingID(); rid != naming.NullRoutingID {
		if entry, names, rejected := c.removeUndialable(ctx, remote, c.ridCache[rid], auth); entry != nil {
			return entry, names, rejected, nil
		}
	}
	k := key(network, address)
	for c.started[k] {
		c.cond.Wait()
		if c.addrCache == nil {
			return nil, nil, nil, NewErrCacheClosed(nil)
		}
	}
	c.started[k] = true
	entry, names, rejected = c.removeUndialable(ctx, remote, c.addrCache[k], auth)
	if entry != nil {
		return entry, names, rejected, nil
	}
	return c.findResolvedLocked(ctx, remote, network, address, auth, p)
}

func (c *ConnCache) findResolvedLocked(ctx *context.T, remote naming.Endpoint, unresNetwork string, unresAddress string,
	auth flow.PeerAuthorizer, p flow.Protocol) (entry *conn.Conn, names []string, rejected []security.RejectedBlessing, err error) {
	network, addresses, err := resolve(ctx, p, unresNetwork, unresAddress)
	if err != nil {
		c.unreserveLocked(unresNetwork, unresAddress)
		return nil, nil, nil, iflow.MaybeWrapError(flow.ErrResolveFailed, ctx, err)
	}
	for _, address := range addresses {
		k := key(network, address)
		entry, names, rejected = c.removeUndialable(ctx, remote, c.addrCache[k], auth)
		if entry != nil {
			return entry, names, rejected, nil
		}
	}
	// No entries for any of the addresses were in the cache.
	return nil, nil, nil, nil
}

func resolve(ctx *context.T, p flow.Protocol, protocol, address string) (string, []string, error) {
	if p != nil {
		net, addrs, err := p.Resolve(ctx, protocol, address)
		if err != nil {
			return "", nil, err
		}
		if len(addrs) > 0 {
			return net, addrs, nil
		}
	}
	return "", nil, NewErrUnknownProtocol(ctx, protocol)
}

// Unreserve marks the status of the (protocol, address) as no longer started, and
// broadcasts waiting threads.
func (c *ConnCache) Unreserve(protocol, address string) {
	c.mu.Lock()
	c.unreserveLocked(protocol, address)
	c.mu.Unlock()
}

func (c *ConnCache) unreserveLocked(protocol, address string) {
	delete(c.started, key(protocol, address))
	c.cond.Broadcast()
}

// Close marks the ConnCache as closed and closes all Conns in the cache.
func (c *ConnCache) Close(ctx *context.T) {
	defer c.mu.Unlock()
	c.mu.Lock()
	c.addrCache, c.started = nil, nil
	err := NewErrCacheClosed(ctx)
	for _, d := range c.ridCache {
		d.conn.Close(ctx, err)
	}
	for d := range c.unmappedConns {
		d.conn.Close(ctx, err)
	}
}

// removeUndialable filters connections that are closed, lameducked, or non-proxied
// connections that do not authorize.
func (c *ConnCache) removeUndialable(ctx *context.T, remote naming.Endpoint, e *connEntry, auth flow.PeerAuthorizer) (*conn.Conn, []string, []security.RejectedBlessing) {
	if e == nil {
		return nil, nil, nil
	}
	if status := e.conn.Status(); status >= conn.Closing || e.conn.RemoteLameDuck() {
		delete(c.addrCache, e.addrKey)
		delete(c.ridCache, e.rid)
		if status < conn.Closing {
			c.unmappedConns[e] = true
		}
		return nil, nil, nil
	}
	if !e.proxy && auth != nil {
		names, rejected, err := auth.AuthorizePeer(ctx,
			e.conn.LocalEndpoint(),
			remote,
			e.conn.RemoteBlessings(),
			e.conn.RemoteDischarges())
		if err != nil {
			return nil, names, rejected
		}
		return e.conn, names, rejected
	}
	return e.conn, nil, nil
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
		if entry, _, _ := c.removeUndialable(ctx, nil, e, nil); entry == nil {
			continue
		}
		if e.conn.IsEncapsulated() {
			// Killing a proxied connection doesn't save us any FD resources, just memory.
			continue
		}
		pq = append(pq, e)
	}
	for d := range c.unmappedConns {
		if status := d.conn.Status(); status == conn.Closed {
			delete(c.unmappedConns, d)
			continue
		}
		if d.conn.IsEncapsulated() {
			continue
		}
		pq = append(pq, d)
	}
	sort.Sort(pq)
	for i := 0; i < num; i++ {
		d := pq[i]
		d.conn.Close(ctx, err)
		delete(c.addrCache, d.addrKey)
		delete(c.ridCache, d.rid)
		delete(c.unmappedConns, d)
	}
	return nil
}

func (c *ConnCache) EnterLameDuckMode(ctx *context.T) {
	c.mu.Lock()
	n := len(c.ridCache) + len(c.unmappedConns)
	conns := make([]*conn.Conn, 0, n)
	for _, e := range c.ridCache {
		conns = append(conns, e.conn)
	}
	for d := range c.unmappedConns {
		conns = append(conns, d.conn)
	}
	c.mu.Unlock()

	waitfor := make([]chan struct{}, 0, n)
	for _, c := range conns {
		waitfor = append(waitfor, c.EnterLameDuck(ctx))
	}
	for _, w := range waitfor {
		<-w
	}
}

// TODO(suharshs): If sorting the connections becomes too slow, switch to
// container/heap instead of sorting all the connections.
type connEntries []*connEntry

func (c connEntries) Len() int {
	return len(c)
}

func (c connEntries) Less(i, j int) bool {
	return c[i].conn.LastUsed().Before(c[j].conn.LastUsed())
}

func (c connEntries) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

func key(protocol, address string) string {
	return protocol + "," + address
}
