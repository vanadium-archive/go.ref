// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
	"time"

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
	idleExpiry    time.Duration
}

type connEntry struct {
	conn    *conn.Conn
	rid     naming.RoutingID
	addrKey string
	proxy   bool
}

// NewConnCache creates a conncache with and idleExpiry for connections.
// If idleExpiry is zero, connections will never expire.
func NewConnCache(idleExpiry time.Duration) *ConnCache {
	mu := &sync.Mutex{}
	cond := sync.NewCond(mu)
	return &ConnCache{
		mu:            mu,
		cond:          cond,
		addrCache:     make(map[string]*connEntry),
		ridCache:      make(map[naming.RoutingID]*connEntry),
		started:       make(map[string]bool),
		unmappedConns: make(map[*connEntry]bool),
		idleExpiry:    idleExpiry,
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
		rid:     ep.RoutingID,
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
		rid:   ep.RoutingID,
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
	auth flow.PeerAuthorizer) (conn *conn.Conn, names []string, rejected []security.RejectedBlessing, err error) {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return nil, nil, nil, NewErrCacheClosed(nil)
	}
	if rid := remote.RoutingID; rid != naming.NullRoutingID {
		if conn, names, rejected := c.removeAndFilterConnBreaksCriticalSection(ctx, remote, c.ridCache[rid], auth); conn != nil {
			return conn, names, rejected, nil
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
	p flow.Protocol) (conn *conn.Conn, names []string, rejected []security.RejectedBlessing, err error) {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return nil, nil, nil, NewErrCacheClosed(nil)
	}
	if rid := remote.RoutingID; rid != naming.NullRoutingID {
		if conn, names, rejected := c.removeAndFilterConnBreaksCriticalSection(ctx, remote, c.ridCache[rid], auth); conn != nil {
			return conn, names, rejected, nil
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
	if conn, names, rejected = c.removeAndFilterConnBreaksCriticalSection(ctx, remote, c.addrCache[k], auth); conn != nil {
		return conn, names, rejected, nil
	}
	return c.findResolvedLocked(ctx, remote, network, address, auth, p)
}

func (c *ConnCache) findResolvedLocked(ctx *context.T, remote naming.Endpoint, unresNetwork string, unresAddress string,
	auth flow.PeerAuthorizer, p flow.Protocol) (conn *conn.Conn, names []string, rejected []security.RejectedBlessing, err error) {
	network, addresses, err := resolve(ctx, p, unresNetwork, unresAddress)
	if err != nil {
		c.unreserveLocked(unresNetwork, unresAddress)
		return nil, nil, nil, iflow.MaybeWrapError(flow.ErrResolveFailed, ctx, err)
	}
	for _, address := range addresses {
		k := key(network, address)
		if conn, names, rejected = c.removeAndFilterConnBreaksCriticalSection(ctx, remote, c.addrCache[k], auth); conn != nil {
			return conn, names, rejected, nil
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
	c.iterateOnConnsLocked(ctx, func(e *connEntry) { e.conn.Close(ctx, err) })
}

// TODO(suharshs): This function starts and ends holding the lock, but releases the lock
// during its execution.
func (c *ConnCache) removeAndFilterConnBreaksCriticalSection(ctx *context.T, remote naming.Endpoint, e *connEntry, auth flow.PeerAuthorizer) (*conn.Conn, []string, []security.RejectedBlessing) {
	if c.removeUndialableLocked(e) {
		return nil, nil, nil
	}
	defer c.mu.Lock()
	c.mu.Unlock()
	return c.filterUnauthorized(ctx, remote, e, auth)
}

// removeUndialableLocked removes connections that are:
// - closed
// - lameducked
// returns true if the connection was removed.
func (c *ConnCache) removeUndialableLocked(e *connEntry) bool {
	if e == nil {
		return true
	}
	if status := e.conn.Status(); status >= conn.Closing || e.conn.RemoteLameDuck() {
		delete(c.addrCache, e.addrKey)
		delete(c.ridCache, e.rid)
		if status < conn.Closing {
			c.unmappedConns[e] = true
		}
		return true
	}
	return false
}

// filterUnauthorized connections that are non-proxied and fail to authorize.
// We only filter unauthorized connections here, rather than removing them from
// the cache because a connection may authorize in regards to another authorizer
// for a different RPC call.
// IMPORTANT: ConnCache.mu.lock should not be held during this function because
// AuthorizePeer calls conn.RemoteDischarges which can hang if a misbehaving
// client fails to send its discharges.
func (c *ConnCache) filterUnauthorized(ctx *context.T, remote naming.Endpoint, e *connEntry, auth flow.PeerAuthorizer) (*conn.Conn, []string, []security.RejectedBlessing) {
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

// KillConnections will closes at least num Conns in the cache.
// This is useful when the manager is approaching system FD limits.
//
// The policy is as follows:
// (1) Remove undialable conns from the cache, there is no point
//     in closing undialable connections to address a FD limit.
// (2) Close and remove lameducked, expired connections from the cache,
//     counting non-proxied connections towards the removed FD count (num).
// (3) LameDuck idle expired connections, killing them if num is still greater
//     than 0.
// (4) Finally if 'num' hasn't been reached, remove the LRU remaining conns
//     until num is reached.
//
// If num is greater than the number of connections in the cache, all cached
// connections will be closed and removed.
// KillConnections returns an error iff the cache is closed.
func (c *ConnCache) KillConnections(ctx *context.T, num int) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(ctx)
	}
	c.removeUndialableConnsLocked(ctx)
	num -= c.killLameDuckedConnsLocked(ctx)
	num = c.lameDuckIdleConnsLocked(ctx, num)
	// If we have killed enough idle connections we can exit early.
	if num <= 0 {
		return nil
	}
	c.killLRUConnsLocked(ctx, num)
	return nil
}

func (c *ConnCache) removeUndialableConnsLocked(ctx *context.T) {
	for _, e := range c.ridCache {
		c.removeUndialableLocked(e)
	}
	for d := range c.unmappedConns {
		if status := d.conn.Status(); status == conn.Closed {
			delete(c.unmappedConns, d)
		}
	}
}

// killLameDuckedConnsLocked kills lameDucked connections, returning the number of
// connection killed.
func (c *ConnCache) killLameDuckedConnsLocked(ctx *context.T) int {
	num := 0
	killLame := func(e *connEntry) {
		if status := e.conn.Status(); status == conn.LameDuckAcknowledged {
			if e.conn.CloseIfIdle(ctx, c.idleExpiry) {
				c.removeEntryLocked(ctx, e)
				num++
			}
		}
	}
	c.iterateOnConnsLocked(ctx, killLame)
	return num
}

// lameDuckIdleConnectionsLocked lameDucks idle expired connections.
// If num > 0, up to num idle connections will be killed instead of lameducked
// to free FD resources.
// Otherwise, the the lameducked connections will be closed when all active
// in subsequent calls of KillConnections, once they become idle.
// lameDuckIdleConnectionsLocked returns the remaining number of connections
// that need to be killed to free FD resources.
func (c *ConnCache) lameDuckIdleConnsLocked(ctx *context.T, num int) int {
	if c.idleExpiry == 0 {
		return num
	}
	lameDuck := func(e *connEntry) {
		// TODO(suharshs): This policy is not ideal as we should try to close everything
		// we can close without potentially losing RPCs first. The ideal policy would
		// close idle client only connections before closing server connections.
		if num > 0 && !e.conn.IsEncapsulated() {
			if e.conn.CloseIfIdle(ctx, c.idleExpiry) {
				num--
			}
			c.removeEntryLocked(ctx, e)
		} else if e.conn.IsIdle(ctx, c.idleExpiry) {
			e.conn.EnterLameDuck(ctx)
		}
	}
	c.iterateOnConnsLocked(ctx, lameDuck)
	return num
}

func (c *ConnCache) killLRUConnsLocked(ctx *context.T, num int) {
	err := NewErrConnKilledToFreeResources(ctx)
	pq := make(connEntries, 0, len(c.ridCache))
	appendIfEncapsulated := func(e *connEntry) {
		// Killing a proxied connection doesn't save us any FD resources, just memory.
		if !e.conn.IsEncapsulated() {
			pq = append(pq, e)
		}
	}
	c.iterateOnConnsLocked(ctx, appendIfEncapsulated)
	sort.Sort(pq)
	for i := 0; i < num && i < len(pq); i++ {
		e := pq[i]
		e.conn.Close(ctx, err)
		c.removeEntryLocked(ctx, e)
	}
}

func (c *ConnCache) EnterLameDuckMode(ctx *context.T) {
	c.mu.Lock()
	n := len(c.ridCache) + len(c.unmappedConns)
	waitfor := make([]chan struct{}, 0, n)
	c.iterateOnConnsLocked(ctx, func(e *connEntry) { waitfor = append(waitfor, e.conn.EnterLameDuck(ctx)) })
	c.mu.Unlock()

	for _, w := range waitfor {
		<-w
	}
}

// iterateOnConns runs fn on each connEntry in the cache in a single thread.
func (c *ConnCache) iterateOnConnsLocked(ctx *context.T, fn func(*connEntry)) {
	for _, e := range c.ridCache {
		fn(e)
	}
	for e := range c.unmappedConns {
		fn(e)
	}
}

func (c *ConnCache) removeEntryLocked(ctx *context.T, e *connEntry) {
	delete(c.addrCache, e.addrKey)
	delete(c.ridCache, e.rid)
	delete(c.unmappedConns, e)
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
