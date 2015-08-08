// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"strconv"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"

	inaming "v.io/x/ref/runtime/internal/naming"
)

func TestCache(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	c := NewConnCache()
	remote := &inaming.Endpoint{
		Protocol:  "tcp",
		Address:   "127.0.0.1:1111",
		RID:       naming.FixedRoutingID(0x5555),
		Blessings: []string{"A", "B", "C"},
	}
	conn := makeConn(t, ctx, remote)
	if err := c.Insert(conn); err != nil {
		t.Fatal(err)
	}
	// We should be able to find the conn in the cache.
	if got, err := c.ReservedFind(remote.Protocol, remote.Address, remote.Blessings); err != nil || got != conn {
		t.Errorf("got %v, want %v, err: %v", got, conn, err)
	}
	c.Unreserve(remote.Protocol, remote.Address, remote.Blessings)
	// Changing the protocol should fail.
	if got, err := c.ReservedFind("wrong", remote.Address, remote.Blessings); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}
	c.Unreserve("wrong", remote.Address, remote.Blessings)
	// Changing the address should fail.
	if got, err := c.ReservedFind(remote.Protocol, "wrong", remote.Blessings); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}
	c.Unreserve(remote.Protocol, "wrong", remote.Blessings)
	// Changing the blessingNames should fail.
	if got, err := c.ReservedFind(remote.Protocol, remote.Address, []string{"wrong"}); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}
	c.Unreserve(remote.Protocol, remote.Address, []string{"wrong"})

	// We should be able to find the conn in the cache by looking up the RoutingID.
	if got, err := c.FindWithRoutingID(remote.RID); err != nil || got != conn {
		t.Errorf("got %v, want %v, err: %v", got, conn, err)
	}
	// Looking up the wrong RID should fail.
	if got, err := c.FindWithRoutingID(naming.FixedRoutingID(0x1111)); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}

	otherEP := &inaming.Endpoint{
		Protocol:  "other",
		Address:   "other",
		Blessings: []string{"other"},
	}
	otherConn := makeConn(t, ctx, otherEP)

	// Looking up a not yet inserted endpoint should fail.
	if got, err := c.ReservedFind(otherEP.Protocol, otherEP.Address, otherEP.Blessings); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}
	// Looking it up again should block until a matching Unreserve call is made.
	ch := make(chan *Conn, 1)
	go func(ch chan *Conn) {
		conn, err := c.ReservedFind(otherEP.Protocol, otherEP.Address, otherEP.Blessings)
		if err != nil {
			t.Fatal(err)
		}
		ch <- conn
	}(ch)

	// We insert the other conn into the cache.
	if err := c.Insert(otherConn); err != nil {
		t.Fatal(err)
	}
	c.Unreserve(otherEP.Protocol, otherEP.Address, otherEP.Blessings)
	// Now the c.ReservedFind should have unblocked and returned the correct Conn.
	if cachedConn := <-ch; cachedConn != otherConn {
		t.Errorf("got %v, want %v", cachedConn, otherConn)
	}

	// Closing the cache should close all the connections in the cache.
	// Ensure that the conns are not closed yet.
	if isClosed(conn) {
		t.Fatalf("wanted conn to not be closed")
	}
	if isClosed(otherConn) {
		t.Fatalf("wanted otherConn to not be closed")
	}
	c.Close(ctx)
	// Now the connections should be closed.
	<-conn.Closed()
	<-otherConn.Closed()
}

func TestLRU(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	// Ensure that the least recently inserted conns are killed by KillConnections.
	c := NewConnCache()
	conns := nConns(t, ctx, 10)
	for _, conn := range conns {
		if err := c.Insert(conn); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.KillConnections(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if !cacheSizeMatches(c) {
		t.Errorf("the size of the caches and LRU list do not match")
	}
	// conns[3:] should not be closed and still in the cache.
	// conns[:3] should be closed and removed from the cache.
	for _, conn := range conns[3:] {
		if isClosed(conn) {
			t.Errorf("conn %v should not have been closed", conn)
		}
		if !isInCache(t, c, conn) {
			t.Errorf("conn %v should still be in cache", conn)
		}
	}
	for _, conn := range conns[:3] {
		<-conn.Closed()
		if isInCache(t, c, conn) {
			t.Errorf("conn %v should not be in cache", conn)
		}
	}

	// Ensure that ReservedFind marks conns as more recently used.
	c = NewConnCache()
	conns = nConns(t, ctx, 10)
	for _, conn := range conns {
		if err := c.Insert(conn); err != nil {
			t.Fatal(err)
		}
	}
	for _, conn := range conns[:7] {
		if got, err := c.ReservedFind(conn.remote.Addr().Network(), conn.remote.Addr().String(), conn.remote.BlessingNames()); err != nil || got != conn {
			t.Errorf("got %v, want %v, err: %v", got, conn, err)
		}
		c.Unreserve(conn.remote.Addr().Network(), conn.remote.Addr().String(), conn.remote.BlessingNames())
	}
	if err := c.KillConnections(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if !cacheSizeMatches(c) {
		t.Errorf("the size of the caches and LRU list do not match")
	}
	// conns[:7] should not be closed and still in the cache.
	// conns[7:] should be closed and removed from the cache.
	for _, conn := range conns[:7] {
		if isClosed(conn) {
			t.Errorf("conn %v should not have been closed", conn)
		}
		if !isInCache(t, c, conn) {
			t.Errorf("conn %v should still be in cache", conn)
		}
	}
	for _, conn := range conns[7:] {
		<-conn.Closed()
		if isInCache(t, c, conn) {
			t.Errorf("conn %v should not be in cache", conn)
		}
	}

	// Ensure that FindWithRoutingID marks conns as more recently used.
	c = NewConnCache()
	conns = nConns(t, ctx, 10)
	for _, conn := range conns {
		if err := c.Insert(conn); err != nil {
			t.Fatal(err)
		}
	}
	for _, conn := range conns[:7] {
		if got, err := c.FindWithRoutingID(conn.remote.RoutingID()); err != nil || got != conn {
			t.Errorf("got %v, want %v, err: %v", got, conn, err)
		}
	}
	if err := c.KillConnections(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if !cacheSizeMatches(c) {
		t.Errorf("the size of the caches and LRU list do not match")
	}
	// conns[:7] should not be closed and still in the cache.
	// conns[7:] should be closed and removed from the cache.
	for _, conn := range conns[:7] {
		if isClosed(conn) {
			t.Errorf("conn %v should not have been closed", conn)
		}
		if !isInCache(t, c, conn) {
			t.Errorf("conn %v should still be in cache", conn)
		}
	}
	for _, conn := range conns[7:] {
		<-conn.Closed()
		if isInCache(t, c, conn) {
			t.Errorf("conn %v should not be in cache", conn)
		}
	}
}

func isInCache(t *testing.T, c *ConnCache, conn *Conn) bool {
	rfconn, err := c.ReservedFind(conn.remote.Addr().Network(), conn.remote.Addr().String(), conn.remote.BlessingNames())
	if err != nil {
		t.Errorf("got %v, want %v, err: %v", rfconn, conn, err)
	}
	c.Unreserve(conn.remote.Addr().Network(), conn.remote.Addr().String(), conn.remote.BlessingNames())
	ridconn, err := c.FindWithRoutingID(conn.remote.RoutingID())
	if err != nil {
		t.Errorf("got %v, want %v, err: %v", ridconn, conn, err)
	}
	return rfconn != nil || ridconn != nil
}

func cacheSizeMatches(c *ConnCache) bool {
	ls := listSize(c)
	return ls == len(c.addrCache) && ls == len(c.ridCache)
}

func listSize(c *ConnCache) int {
	size := 0
	d := c.head.next
	for d != c.head {
		size++
		d = d.next
	}
	return size
}

func nConns(t *testing.T, ctx *context.T, n int) []*Conn {
	conns := make([]*Conn, n)
	for i := 0; i < n; i++ {
		conns[i] = makeConn(t, ctx, &inaming.Endpoint{
			Protocol: strconv.Itoa(i),
			RID:      naming.FixedRoutingID(uint64(i)),
		})
	}
	return conns
}

func makeConn(t *testing.T, ctx *context.T, ep naming.Endpoint) *Conn {
	d, _, _ := newMRWPair(ctx)
	c, err := NewDialed(ctx, d, ep, ep, version.RPCVersionRange{Min: 1, Max: 5}, nil, nil)
	if err != nil {
		t.Fatalf("Could not create conn: %v", err)
	}
	return c
}
