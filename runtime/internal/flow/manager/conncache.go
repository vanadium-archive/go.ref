// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/x/ref/lib/stats"
	"v.io/x/ref/runtime/internal/flow/conn"
)

// ConnCache is a cache from (protocol, address) and (routingID) to a set of Conns.
// Multiple goroutines may invoke methods on the ConnCache simultaneously.
type ConnCache struct {
	mu        *sync.Mutex
	cond      *sync.Cond
	addrCache map[string][]*connEntry           // keyed by "protocol,address"
	ridCache  map[naming.RoutingID][]*connEntry // keyed by remote.RoutingID
	started   map[string]bool                   // keyed by "protocol,address"

	idleExpiry time.Duration
}

type connEntry struct {
	conn    cachedConn
	rid     naming.RoutingID
	addrKey string
	proxy   bool
}

// cachedConn is the interface implemented by *conn.Conn that is used by ConnCache.
// We make the ConnCache API take this interface to make testing easier.
type cachedConn interface {
	Status() conn.Status
	IsEncapsulated() bool
	IsIdle(*context.T, time.Duration) bool
	EnterLameDuck(*context.T) chan struct{}
	RemoteLameDuck() bool
	CloseIfIdle(*context.T, time.Duration) bool
	Close(*context.T, error)
	RemoteEndpoint() naming.Endpoint
	LocalEndpoint() naming.Endpoint
	RemoteBlessings() security.Blessings
	RemoteDischarges() map[string]security.Discharge
	RTT() time.Duration
	LastUsed() time.Time
	DebugString() string
}

// NewConnCache creates a ConnCache with an idleExpiry for connections.
// If idleExpiry is zero, connections will never expire.
func NewConnCache(idleExpiry time.Duration) *ConnCache {
	mu := &sync.Mutex{}
	cond := sync.NewCond(mu)
	return &ConnCache{
		mu:         mu,
		cond:       cond,
		addrCache:  make(map[string][]*connEntry),
		ridCache:   make(map[naming.RoutingID][]*connEntry),
		started:    make(map[string]bool),
		idleExpiry: idleExpiry,
	}
}

// Insert adds conn to the cache, keyed by both (protocol, address) and (routingID).
// An error will be returned iff the cache has been closed.
func (c *ConnCache) Insert(conn cachedConn, proxy bool) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(nil)
	}
	c.insertConnLocked(conn, proxy, true)
	return nil
}

// InsertWithRoutingID adds conn to the cache keyed only by conn's RoutingID.
func (c *ConnCache) InsertWithRoutingID(conn cachedConn, proxy bool) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return NewErrCacheClosed(nil)
	}
	c.insertConnLocked(conn, proxy, false)
	return nil
}

// Find returns a Conn based on the input remoteEndpoint.
// nil is returned if there is no such Conn.
//
// If no error is returned, the caller is required to call Unreserve, to avoid
// deadlock. The (network, address) provided to Unreserve must be the same as
// the arguments provided to Find.
// All new Find calls for the (network, address) will Block
// until the corresponding Unreserve call is made.
// p is used to check the cache for resolved protocols.
func (c *ConnCache) Find(ctx *context.T, remote naming.Endpoint, network, address string, auth flow.PeerAuthorizer,
	p flow.Protocol) (conn cachedConn, names []string, rejected []security.RejectedBlessing, err error) {
	if conn, names, rejected, err = c.findWithRoutingID(ctx, remote, auth); err != nil || conn != nil {
		return conn, names, rejected, err
	}
	if conn, names, rejected, err = c.findWithAddress(ctx, remote, network, address, auth); err != nil || conn != nil {
		return conn, names, rejected, err
	}
	return c.findWithResolvedAddress(ctx, remote, network, address, auth, p)
}

// Find returns a Conn based only on the RoutingID of remote.
func (c *ConnCache) FindWithRoutingID(ctx *context.T, remote naming.Endpoint,
	auth flow.PeerAuthorizer) (cachedConn, []string, []security.RejectedBlessing, error) {
	return c.findWithRoutingID(ctx, remote, auth)
}

// Unreserve marks the status of the (network, address) as no longer started, and
// broadcasts waiting threads to continue with their halted Find call.
func (c *ConnCache) Unreserve(network, address string) {
	defer c.mu.Unlock()
	c.mu.Lock()
	delete(c.started, key(network, address))
	c.cond.Broadcast()
}

// KillConnections will closes at least num Conns in the cache.
// This is useful when the manager is approaching system FD limits.
//
// The policy is as follows:
// (1) Remove undialable (closing/closed) conns from the cache, there is no point
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
	// entries will be at least c.ridCache size.
	entries := make(lruEntries, 0, len(c.ridCache))
	for _, es := range c.ridCache {
		for _, e := range es {
			entries = append(entries, e)
		}
	}
	k := 0
	for _, e := range entries {
		if status := e.conn.Status(); status >= conn.Closing {
			// Remove undialable conns.
			c.removeEntryLocked(e)
		} else if status == conn.LameDuckAcknowledged && e.conn.CloseIfIdle(ctx, c.idleExpiry) {
			// Close and remove lameducked or idle connections.
			c.removeEntryLocked(e)
			num--
		} else {
			entries[k] = e
			k++
		}
	}
	entries = entries[:k]
	// Lameduck or kill idle connections.
	// If num > 0, up to num idle connections will be killed instead of lameducked
	// to free FD resources.
	// Otherwise, the the lameducked connections will be closed when all active
	// in subsequent calls of KillConnections, once they become idle.
	// TODO(suharshs): This policy is not ideal as we should try to close everything
	// we can close without potentially losing RPCs first. The ideal policy would
	// close idle client only connections before closing server connections.
	k = 0
	for _, e := range entries {
		// Kill idle connections.
		if num > 0 && !e.conn.IsEncapsulated() && e.conn.CloseIfIdle(ctx, c.idleExpiry) {
			num--
			c.removeEntryLocked(e)
			continue
		}
		// Lameduck idle connections.
		if e.conn.IsIdle(ctx, c.idleExpiry) {
			e.conn.EnterLameDuck(ctx)
		}
		// No point in closing encapsulated connections when we reach an FD limit.
		if !e.conn.IsEncapsulated() {
			entries[k] = e
			k++
		}
	}
	entries = entries[:k]

	// If we have killed enough idle connections we can exit early.
	if num <= 0 {
		return nil
	}
	// Otherwise we need to kill the LRU conns.
	sort.Sort(entries)
	err := NewErrConnKilledToFreeResources(ctx)
	for i := 0; i < num && i < len(entries); i++ {
		e := entries[i]
		e.conn.Close(ctx, err)
		c.removeEntryLocked(e)
	}
	return nil
}

// EnterLameDuckMode lame ducks all connections and waits for the the remote
// end to acknowledge the lameduck.
func (c *ConnCache) EnterLameDuckMode(ctx *context.T) {
	c.mu.Lock()
	// waitfor will be at least c.ridCache size.
	waitfor := make([]chan struct{}, 0, len(c.ridCache))
	for _, entries := range c.ridCache {
		for _, e := range entries {
			waitfor = append(waitfor, e.conn.EnterLameDuck(ctx))
		}
	}
	c.mu.Unlock()
	for _, w := range waitfor {
		<-w
	}
}

// Close closes all connections in the cache.
func (c *ConnCache) Close(ctx *context.T) {
	defer c.mu.Unlock()
	c.mu.Lock()
	err := NewErrCacheClosed(ctx)
	for _, entries := range c.ridCache {
		for _, e := range entries {
			e.conn.Close(ctx, err)
		}
	}
	c.addrCache = nil
	c.ridCache = nil
	c.started = nil
}

// String returns a user friendly representation of the connections in the cache.
func (c *ConnCache) String() string {
	defer c.mu.Unlock()
	c.mu.Lock()
	buf := &bytes.Buffer{}
	if c.addrCache == nil {
		return "conncache closed"
	}
	fmt.Fprintln(buf, "AddressCache:")
	for k, entries := range c.addrCache {
		for _, e := range entries {
			fmt.Fprintf(buf, "%v: %p\n", k, e.conn)
		}
	}
	fmt.Fprintln(buf, "RIDCache:")
	for k, entries := range c.ridCache {
		for _, e := range entries {
			fmt.Fprintf(buf, "%v: %p\n", k, e.conn)
		}
	}
	return buf.String()
}

// ExportStats exports cache information to the global stats.
func (c *ConnCache) ExportStats(prefix string) {
	stats.NewStringFunc(naming.Join(prefix, "addr"), func() string { return c.debugStringForAddrCache() })
	stats.NewStringFunc(naming.Join(prefix, "rid"), func() string { return c.debugStringForRIDCache() })
	stats.NewStringFunc(naming.Join(prefix, "dialing"), func() string { return c.debugStringForDialing() })
}

func (c *ConnCache) insertConnLocked(conn cachedConn, proxy bool, keyByAddr bool) {
	ep := conn.RemoteEndpoint()
	entry := &connEntry{
		conn:  conn,
		rid:   ep.RoutingID,
		proxy: proxy,
	}
	if keyByAddr {
		addr := ep.Addr()
		k := key(addr.Network(), addr.String())
		entry.addrKey = k
		c.addrCache[k] = append(c.addrCache[k], entry)
	}
	c.ridCache[entry.rid] = append(c.ridCache[entry.rid], entry)
}

func (c *ConnCache) findWithRoutingID(ctx *context.T, remote naming.Endpoint,
	auth flow.PeerAuthorizer) (cachedConn, []string, []security.RejectedBlessing, error) {
	c.mu.Lock()
	if c.addrCache == nil {
		c.mu.Unlock()
		return nil, nil, nil, NewErrCacheClosed(ctx)
	}
	rid := remote.RoutingID
	if rid == naming.NullRoutingID {
		c.mu.Unlock()
		return nil, nil, nil, nil
	}
	entries := c.makeRTTEntriesLocked(c.ridCache[rid])
	c.mu.Unlock()

	conn, names, rejected := c.pickFirstAuthorizedConn(ctx, remote, entries, auth)
	return conn, names, rejected, nil
}

func (c *ConnCache) findWithAddress(ctx *context.T, remote naming.Endpoint, network, address string,
	auth flow.PeerAuthorizer) (cachedConn, []string, []security.RejectedBlessing, error) {
	k := key(network, address)

	c.mu.Lock()
	if c.addrCache == nil {
		c.mu.Unlock()
		return nil, nil, nil, NewErrCacheClosed(ctx)
	}
	for c.started[k] {
		c.cond.Wait()
		if c.addrCache == nil {
			c.mu.Unlock()
			return nil, nil, nil, NewErrCacheClosed(ctx)
		}
	}
	c.started[k] = true
	entries := c.makeRTTEntriesLocked(c.addrCache[k])
	c.mu.Unlock()

	conn, names, rejected := c.pickFirstAuthorizedConn(ctx, remote, entries, auth)
	return conn, names, rejected, nil
}

func (c *ConnCache) findWithResolvedAddress(ctx *context.T, remote naming.Endpoint, network, address string,
	auth flow.PeerAuthorizer, p flow.Protocol) (cachedConn, []string, []security.RejectedBlessing, error) {
	network, addresses, err := resolve(ctx, p, network, address)
	if err != nil {
		// TODO(suharshs): Add a unittest for failed resolution.
		ctx.VI(2).Infof("Failed to resolve (%v, %v): %v", network, address, err)
		return nil, nil, nil, nil
	}

	for _, address := range addresses {
		c.mu.Lock()
		if c.addrCache == nil {
			c.mu.Unlock()
			return nil, nil, nil, NewErrCacheClosed(ctx)
		}
		k := key(network, address)
		entries := c.makeRTTEntriesLocked(c.addrCache[k])
		c.mu.Unlock()

		if conn, names, rejected := c.pickFirstAuthorizedConn(ctx, remote, entries, auth); conn != nil {
			return conn, names, rejected, nil
		}
	}

	return nil, nil, nil, nil
}

func (c *ConnCache) makeRTTEntriesLocked(es []*connEntry) rttEntries {
	if len(es) == 0 {
		return nil
	}
	// Sort connections by RTT.
	entries := make(rttEntries, len(es))
	copy(entries, es)
	sort.Sort(entries)
	// Remove undialable connections.
	k := 0
	for _, e := range entries {
		if status := e.conn.Status(); status >= conn.Closing {
			c.removeEntryLocked(e)
		} else if !e.conn.RemoteLameDuck() {
			entries[k] = e
			k++
		}
	}
	return entries[:k]
}

func (c *ConnCache) pickFirstAuthorizedConn(ctx *context.T, remote naming.Endpoint,
	entries rttEntries, auth flow.PeerAuthorizer) (cachedConn, []string, []security.RejectedBlessing) {
	for _, e := range entries {
		if e.proxy || auth == nil {
			return e.conn, nil, nil
		}
		names, rejected, err := auth.AuthorizePeer(ctx,
			e.conn.LocalEndpoint(),
			remote,
			e.conn.RemoteBlessings(),
			e.conn.RemoteDischarges())
		if err == nil {
			return e.conn, names, rejected
		}
	}
	return nil, nil, nil
}

func (c *ConnCache) removeEntryLocked(entry *connEntry) {
	addrConns, ok := c.addrCache[entry.addrKey]
	if ok {
		addrConns = removeEntryFromSlice(addrConns, entry)
		if len(addrConns) == 0 {
			delete(c.addrCache, entry.addrKey)
		} else {
			c.addrCache[entry.addrKey] = addrConns
		}
	}
	ridConns, ok := c.ridCache[entry.rid]
	if ok {
		ridConns = removeEntryFromSlice(ridConns, entry)
		if len(ridConns) == 0 {
			delete(c.ridCache, entry.rid)
		} else {
			c.ridCache[entry.rid] = ridConns
		}
	}
}

func removeEntryFromSlice(entries []*connEntry, entry *connEntry) []*connEntry {
	for i, e := range entries {
		if e == entry {
			n := len(entries)
			entries[i], entries = entries[n-1], entries[:n-1]
			break
		}
	}
	return entries
}

func (c *ConnCache) debugStringForAddrCache() string {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.addrCache == nil {
		return "<closed>"
	}
	buf := &bytes.Buffer{}
	// map iteration is unstable, so sort the keys first
	keys := make([]string, len(c.addrCache))
	i := 0
	for k := range c.addrCache {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(buf, "KEY: %v\n", k)
		for _, e := range c.addrCache[k] {
			fmt.Fprintf(buf, "%v\n", e.conn.DebugString())
		}
		fmt.Fprintf(buf, "\n")
	}
	return buf.String()
}

func (c *ConnCache) debugStringForRIDCache() string {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.ridCache == nil {
		return "<closed>"
	}
	buf := &bytes.Buffer{}
	for k, entries := range c.ridCache {
		fmt.Fprintf(buf, "KEY: %v\n", k)
		for _, e := range entries {
			fmt.Fprintf(buf, "%v\n", e.conn.DebugString())
		}
		fmt.Fprintf(buf, "\n")
	}
	return buf.String()
}

func (c *ConnCache) debugStringForDialing() string {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.started == nil {
		return "<closed>"
	}
	keys := make([]string, len(c.started))
	i := 0
	for k := range c.started {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n")
}

func key(protocol, address string) string {
	return protocol + "," + address
}

type rttEntries []*connEntry

func (e rttEntries) Len() int {
	return len(e)
}

func (e rttEntries) Less(i, j int) bool {
	return e[i].conn.RTT() < e[j].conn.RTT()
}

func (e rttEntries) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

type lruEntries []*connEntry

func (e lruEntries) Len() int {
	return len(e)
}

func (e lruEntries) Less(i, j int) bool {
	return e[i].conn.LastUsed().Before(e[j].conn.LastUsed())
}

func (e lruEntries) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
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
