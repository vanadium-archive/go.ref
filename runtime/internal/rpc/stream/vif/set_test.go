// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif_test

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"testing"
	"time"

	"v.io/x/lib/set"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/verror"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/runtime/internal/rpc/stream/vif"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

var supportsIPv6 bool

func init() {
	simpleResolver := func(ctx *context.T, network, address string) (string, string, error) {
		return network, address, nil
	}
	simpleDial := func(ctx *context.T, p, a string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(p, a, timeout)
	}
	simpleListen := func(ctx *context.T, p, a string) (net.Listener, error) {
		return net.Listen(p, a)
	}
	rpc.RegisterProtocol("unix", simpleDial, simpleResolver, simpleListen)

	// Check whether the platform supports IPv6.
	ln, err := net.Listen("tcp6", "[::1]:0")
	defer ln.Close()
	if err == nil {
		supportsIPv6 = true
	}
}

func newConn(network, address string) (net.Conn, net.Conn, error) {
	dfunc, _, lfunc, _ := rpc.RegisteredProtocol(network)
	ln, err := lfunc(nil, network, address)
	if err != nil {
		return nil, nil, err
	}
	defer ln.Close()

	done := make(chan net.Conn)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		conn.Read(make([]byte, 1)) // Read a dummy byte.
		done <- conn
	}()

	conn, err := dfunc(nil, ln.Addr().Network(), ln.Addr().String(), 1*time.Second)
	if err != nil {
		return nil, nil, err
	}
	// Write a dummy byte since wsh listener waits for the magic bytes for ws.
	conn.Write([]byte("."))
	return conn, <-done, nil
}

func newVIF(ctx *context.T, c, s net.Conn) (*vif.VIF, *vif.VIF, error) {
	done := make(chan *vif.VIF)
	go func() {
		principal := testutil.NewPrincipal("accepted")
		ctx, _ = v23.WithPrincipal(ctx, principal)
		blessings := principal.BlessingStore().Default()
		vf, err := vif.InternalNewAcceptedVIF(ctx, s, naming.FixedRoutingID(0x5), blessings, nil, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR 2: %s\n", verror.DebugString(err))
			panic(err)
		}
		done <- vf
	}()

	ctx, _ = v23.WithPrincipal(ctx, testutil.NewPrincipal("dialed"))
	vf, err := vif.InternalNewDialedVIF(ctx, c, naming.FixedRoutingID(0xc), nil, nil)
	if err != nil {
		return nil, nil, err
	}
	return vf, <-done, nil
}

func diff(a, b []string) []string {
	s1, s2 := set.String.FromSlice(a), set.String.FromSlice(b)
	set.String.Difference(s1, s2)
	return set.String.ToSlice(s1)
}

func find(set *vif.Set, n, a string) *vif.VIF {
	found, unblock := set.BlockingFind(n, a)
	unblock()
	return found
}

func TestSetBasic(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	sockdir, err := ioutil.TempDir("", "TestSetBasic")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockdir)

	all := rpc.RegisteredProtocols()
	unknown := naming.UnknownProtocol
	tests := []struct {
		network, address string
		compatibles      []string
	}{
		{"tcp", "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{"tcp4", "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{"tcp", "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{"tcp6", "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{"ws", "127.0.0.1:0", []string{"ws", "ws4", "wsh", "wsh4", unknown}},
		{"ws4", "127.0.0.1:0", []string{"ws", "ws4", "wsh", "wsh4", unknown}},
		{"ws", "[::1]:0", []string{"ws", "ws6", "wsh", "wsh6", unknown}},
		{"ws6", "[::1]:0", []string{"ws", "ws6", "wsh", "wsh6", unknown}},
		// wsh dial always uses tcp.
		{"wsh", "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{"wsh4", "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{"wsh", "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{"wsh6", "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{unknown, "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{unknown, "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{"unix", path.Join(sockdir, "socket"), []string{"unix"}},
	}

	set := vif.NewSet()
	for _, test := range tests {
		if test.address == "[::1]:0" && !supportsIPv6 {
			continue
		}

		name := fmt.Sprintf("(%q, %q)", test.network, test.address)

		c, s, err := newConn(test.network, test.address)
		if err != nil {
			t.Fatal(err)
		}
		vf, _, err := newVIF(ctx, c, s)
		if err != nil {
			t.Fatal(err)
		}
		a := c.RemoteAddr()

		set.Insert(vf, a.Network(), a.String())
		for _, n := range test.compatibles {
			if found := find(set, n, a.String()); found == nil {
				t.Fatalf("%s: Got nil, but want [%v] on find(%q, %q))", name, vf, n, a)
			}
		}

		for _, n := range diff(all, test.compatibles) {
			if v := find(set, n, a.String()); v != nil {
				t.Fatalf("%s: Got [%v], but want nil on find(%q, %q))", name, v, n, a)
			}
		}

		set.Delete(vf)
		for _, n := range all {
			if v := find(set, n, a.String()); v != nil {
				t.Fatalf("%s: Got [%v], but want nil on find(%q, %q))", name, v, n, a)
			}
		}
	}
}

func TestSetWithPipes(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	a1 := c1.RemoteAddr()
	a2 := c2.RemoteAddr()
	if a1.Network() != a2.Network() || a1.String() != a2.String() {
		t.Fatalf("This test was intended for distinct connections that have duplicate RemoteAddrs. "+
			"That does not seem to be the case with (%q, %q) and (%q, %q)",
			a1.Network(), a1, a2.Network(), a2)
	}

	vf1, _, err := newVIF(ctx, c1, s1)
	if err != nil {
		t.Fatal(err)
	}
	vf2, _, err := newVIF(ctx, c2, s2)
	if err != nil {
		t.Fatal(err)
	}

	set := vif.NewSet()
	set.Insert(vf1, a1.Network(), a1.String())
	if v := find(set, a1.Network(), a1.String()); v != nil {
		t.Fatalf("Got [%v], but want nil on find(%q, %q))", v, a1.Network(), a1)
	}
	if l := set.List(); len(l) != 1 || l[0] != vf1 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set.Insert(vf2, a2.Network(), a2.String())
	if v := find(set, a2.Network(), a2.String()); v != nil {
		t.Fatalf("Got [%v], but want nil on find(%q, %q))", v, a2.Network(), a2)
	}
	if l := set.List(); len(l) != 2 || l[0] != vf1 || l[1] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set.Delete(vf1)
	if l := set.List(); len(l) != 1 || l[0] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
	set.Delete(vf2)
	if l := set.List(); len(l) != 0 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
}

func TestSetWithUnixSocket(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	dir, err := ioutil.TempDir("", "TestSetWithUnixSocket")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c1, s1, err := newConn("unix", path.Join(dir, "socket1"))
	if err != nil {
		t.Fatal(err)
	}
	c2, s2, err := newConn("unix", path.Join(dir, "socket2"))
	if err != nil {
		t.Fatal(err)
	}

	// The client side address is always unix:@ regardless of socket name.
	a1 := s1.RemoteAddr()
	a2 := s2.RemoteAddr()
	if a1.Network() != a2.Network() || a1.String() != a2.String() {
		t.Fatalf("This test was intended for distinct connections that have duplicate RemoteAddrs. "+
			"That does not seem to be the case with (%q, %q) and (%q, %q)",
			a1.Network(), a1, a2.Network(), a2)
	}

	_, vf1, err := newVIF(ctx, c1, s1)
	if err != nil {
		t.Fatal(err)
	}
	_, vf2, err := newVIF(ctx, c2, s2)
	if err != nil {
		t.Fatal(err)
	}

	set := vif.NewSet()
	set.Insert(vf1, a1.Network(), a1.String())
	if v := find(set, a1.Network(), a1.String()); v != nil {
		t.Fatalf("Got [%v], but want nil on find(%q, %q))", v, a1.Network(), a1)
	}
	if l := set.List(); len(l) != 1 || l[0] != vf1 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set.Insert(vf2, a2.Network(), a2.String())
	if v := find(set, a2.Network(), a2.String()); v != nil {
		t.Fatalf("Got [%v], but want nil on find(%q, %q))", v, a2.Network(), a2)
	}
	if l := set.List(); len(l) != 2 || l[0] != vf1 || l[1] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set.Delete(vf1)
	if l := set.List(); len(l) != 1 || l[0] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
	set.Delete(vf2)
	if l := set.List(); len(l) != 0 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
}

func TestSetInsertDelete(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	c1, s1 := net.Pipe()
	vf1, _, err := newVIF(ctx, c1, s1)
	if err != nil {
		t.Fatal(err)
	}

	set1 := vif.NewSet()

	n1, a1 := c1.RemoteAddr().Network(), c1.RemoteAddr().String()
	set1.Insert(vf1, n1, a1)
	if l := set1.List(); len(l) != 1 || l[0] != vf1 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set1.Delete(vf1)
	if l := set1.List(); len(l) != 0 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
}

func TestBlockingFind(t *testing.T) {
	ctx, shutdown := test.V23InitSimple()
	defer shutdown()
	network, address := "tcp", "127.0.0.1:1234"
	set := vif.NewSet()

	_, unblock := set.BlockingFind(network, address)

	ch := make(chan *vif.VIF, 1)

	// set.BlockingFind should block until set.Unblock is called with the corresponding VIF,
	// since set.BlockingFind was called earlier.
	go func(ch chan *vif.VIF) {
		vf, _ := set.BlockingFind(network, address)
		ch <- vf
	}(ch)

	// set.BlockingFind for a different network and address should not block.
	set.BlockingFind("network", "address")

	// Create and insert the VIF.
	c, s, err := newConn(network, address)
	if err != nil {
		t.Fatal(err)
	}
	vf, _, err := newVIF(ctx, c, s)
	if err != nil {
		t.Fatal(err)
	}
	set.Insert(vf, network, address)
	unblock()

	// Now the set.BlockingFind should have returned the correct vif.
	if cachedVif := <-ch; cachedVif != vf {
		t.Errorf("got %v, want %v", cachedVif, vf)
	}
}
