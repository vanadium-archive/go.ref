// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"strconv"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	connpackage "v.io/x/ref/runtime/internal/flow/conn"
	"v.io/x/ref/runtime/internal/flow/flowtest"
	_ "v.io/x/ref/runtime/internal/flow/protocols/local"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/test/goroutines"
)

func TestCache(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	ctx, shutdown := v23.Init()
	defer shutdown()

	c := NewConnCache()
	remote := &inaming.Endpoint{
		Protocol:  "tcp",
		Address:   "127.0.0.1:1111",
		RID:       naming.FixedRoutingID(0x5555),
		Blessings: []string{"A", "B", "C"},
	}
	caf := makeConnAndFlow(t, ctx, remote)
	defer caf.stop(ctx)
	conn := caf.c
	if err := c.Insert(conn, remote.Protocol, remote.Address); err != nil {
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

	// Caching with InsertWithRoutingID should only cache by RoutingID, not with network/address.
	ridEP := &inaming.Endpoint{
		Protocol:  "ridonly",
		Address:   "ridonly",
		RID:       naming.FixedRoutingID(0x1111),
		Blessings: []string{"ridonly"},
	}
	caf = makeConnAndFlow(t, ctx, ridEP)
	defer caf.stop(ctx)
	ridConn := caf.c
	if err := c.InsertWithRoutingID(ridConn); err != nil {
		t.Fatal(err)
	}
	if got, err := c.ReservedFind(ridEP.Protocol, ridEP.Address, ridEP.Blessings); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}
	c.Unreserve(ridEP.Protocol, ridEP.Address, ridEP.Blessings)
	if got, err := c.FindWithRoutingID(ridEP.RID); err != nil || got != ridConn {
		t.Errorf("got %v, want %v, err: %v", got, ridConn, err)
	}

	otherEP := &inaming.Endpoint{
		Protocol:  "other",
		Address:   "other",
		RID:       naming.FixedRoutingID(0x2222),
		Blessings: []string{"other"},
	}
	caf = makeConnAndFlow(t, ctx, otherEP)
	defer caf.stop(ctx)
	otherConn := caf.c

	// Looking up a not yet inserted endpoint should fail.
	if got, err := c.ReservedFind(otherEP.Protocol, otherEP.Address, otherEP.Blessings); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}
	// Looking it up again should block until a matching Unreserve call is made.
	ch := make(chan *connpackage.Conn, 1)
	go func(ch chan *connpackage.Conn) {
		conn, err := c.ReservedFind(otherEP.Protocol, otherEP.Address, otherEP.Blessings)
		if err != nil {
			t.Fatal(err)
		}
		ch <- conn
	}(ch)

	// We insert the other conn into the cache.
	if err := c.Insert(otherConn, otherEP.Protocol, otherEP.Address); err != nil {
		t.Fatal(err)
	}
	c.Unreserve(otherEP.Protocol, otherEP.Address, otherEP.Blessings)
	// Now the c.ReservedFind should have unblocked and returned the correct Conn.
	if cachedConn := <-ch; cachedConn != otherConn {
		t.Errorf("got %v, want %v", cachedConn, otherConn)
	}

	// Insert a duplicate conn to ensure that replaced conns still get closed.
	caf = makeConnAndFlow(t, ctx, remote)
	defer caf.stop(ctx)
	dupConn := caf.c
	if err := c.Insert(dupConn, remote.Protocol, remote.Address); err != nil {
		t.Fatal(err)
	}

	// Closing the cache should close all the connections in the cache.
	// Ensure that the conns are not closed yet.
	if status := conn.Status(); status == connpackage.Closed {
		t.Fatal("wanted conn to not be closed")
	}
	if status := dupConn.Status(); status == connpackage.Closed {
		t.Fatal("wanted dupConn to not be closed")
	}
	if status := otherConn.Status(); status == connpackage.Closed {
		t.Fatal("wanted otherConn to not be closed")
	}
	c.Close(ctx)
	// Now the connections should be closed.
	<-conn.Closed()
	<-ridConn.Closed()
	<-dupConn.Closed()
	<-otherConn.Closed()
}

func TestLRU(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	ctx, shutdown := v23.Init()
	defer shutdown()

	// Ensure that the least recently created conns are killed by KillConnections.
	c := NewConnCache()
	conns, stop := nConnAndFlows(t, ctx, 10)
	defer stop()
	for _, conn := range conns {
		addr := conn.c.RemoteEndpoint().Addr()
		if err := c.Insert(conn.c, addr.Network(), addr.String()); err != nil {
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
		if status := conn.c.Status(); status == connpackage.Closed {
			t.Errorf("conn %v should not have been closed", conn)
		}
		if !isInCache(t, c, conn.c) {
			t.Errorf("conn %v should still be in cache", conn)
		}
	}
	for _, conn := range conns[:3] {
		<-conn.c.Closed()
		if isInCache(t, c, conn.c) {
			t.Errorf("conn %v should not be in cache", conn)
		}
	}

	// Ensure that writing to conns marks conns as more recently used.
	c = NewConnCache()
	conns, stop = nConnAndFlows(t, ctx, 10)
	defer stop()
	for _, conn := range conns {
		addr := conn.c.RemoteEndpoint().Addr()
		if err := c.Insert(conn.c, addr.Network(), addr.String()); err != nil {
			t.Fatal(err)
		}
	}
	for _, conn := range conns[:7] {
		conn.write()
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
		if status := conn.c.Status(); status == connpackage.Closed {
			t.Errorf("conn %v should not have been closed", conn)
		}
		if !isInCache(t, c, conn.c) {
			t.Errorf("conn %v should still be in cache", conn)
		}
	}
	for _, conn := range conns[7:] {
		<-conn.c.Closed()
		if isInCache(t, c, conn.c) {
			t.Errorf("conn %v should not be in cache", conn)
		}
	}

	// Ensure that reading from conns marks conns as more recently used.
	c = NewConnCache()
	conns, stop = nConnAndFlows(t, ctx, 10)
	defer stop()
	for _, conn := range conns {
		addr := conn.c.RemoteEndpoint().Addr()
		if err := c.Insert(conn.c, addr.Network(), addr.String()); err != nil {
			t.Fatal(err)
		}
	}
	for _, conn := range conns[:7] {
		conn.read()
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
		if status := conn.c.Status(); status == connpackage.Closed {
			t.Errorf("conn %v should not have been closed", conn)
		}
		if !isInCache(t, c, conn.c) {
			t.Errorf("conn %v should still be in cache", conn)
		}
	}
	for _, conn := range conns[7:] {
		<-conn.c.Closed()
		if isInCache(t, c, conn.c) {
			t.Errorf("conn %v should not be in cache", conn)
		}
	}
}

func isInCache(t *testing.T, c *ConnCache, conn *connpackage.Conn) bool {
	rep := conn.RemoteEndpoint()
	rfconn, err := c.ReservedFind(rep.Addr().Network(), rep.Addr().String(), rep.BlessingNames())
	if err != nil {
		t.Error(err)
	}
	c.Unreserve(rep.Addr().Network(), rep.Addr().String(), rep.BlessingNames())
	ridconn, err := c.FindWithRoutingID(rep.RoutingID())
	if err != nil {
		t.Error(err)
	}
	return rfconn != nil || ridconn != nil
}

func cacheSizeMatches(c *ConnCache) bool {
	return len(c.addrCache) == len(c.ridCache)
}

type connAndFlow struct {
	c *connpackage.Conn
	a *connpackage.Conn
	f flow.Flow
}

func (c connAndFlow) write() {
	_, err := c.f.WriteMsg([]byte{0})
	if err != nil {
		panic(err)
	}
}

func (c connAndFlow) read() {
	_, err := c.f.ReadMsg()
	if err != nil {
		panic(err)
	}
}

func (c connAndFlow) stop(ctx *context.T) {
	c.c.Close(ctx, nil)
	c.a.Close(ctx, nil)
}

func nConnAndFlows(t *testing.T, ctx *context.T, n int) ([]connAndFlow, func()) {
	cfs := make([]connAndFlow, n)
	for i := 0; i < n; i++ {
		cfs[i] = makeConnAndFlow(t, ctx, &inaming.Endpoint{
			Protocol: strconv.Itoa(i),
			RID:      naming.FixedRoutingID(uint64(i + 1)), // We need to have a nonzero rid for bidi.
		})
	}
	return cfs, func() {
		for _, conn := range cfs {
			conn.stop(ctx)
		}
	}
}

func makeConnAndFlow(t *testing.T, ctx *context.T, ep naming.Endpoint) connAndFlow {
	dmrw, amrw := flowtest.Pipe(t, ctx, "local", "")
	dch := make(chan *connpackage.Conn)
	ach := make(chan *connpackage.Conn)
	lBlessings := v23.GetPrincipal(ctx).BlessingStore().Default()
	go func() {
		d, err := connpackage.NewDialed(ctx, lBlessings, dmrw, ep, ep,
			version.RPCVersionRange{Min: 1, Max: 5},
			flowtest.AllowAllPeersAuthorizer{},
			time.Minute, 0, nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		dch <- d
	}()
	fh := fh{t, make(chan struct{})}
	go func() {
		a, err := connpackage.NewAccepted(ctx, lBlessings, nil, amrw, ep,
			version.RPCVersionRange{Min: 1, Max: 5}, time.Minute, 0, fh)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		ach <- a
	}()
	conn := <-dch
	aconn := <-ach
	f, err := conn.Dial(ctx, flowtest.AllowAllPeersAuthorizer{}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Write a byte to send the openFlow message.
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	<-fh.ch
	return connAndFlow{conn, aconn, f}
}

type fh struct {
	t  *testing.T
	ch chan struct{}
}

func (h fh) HandleFlow(f flow.Flow) error {
	go func() {
		if _, err := f.WriteMsg([]byte{0}); err != nil {
			h.t.Errorf("failed to write: %v", err)
		}
		close(h.ch)
	}()
	return nil
}
