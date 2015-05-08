// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy_test

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"

	_ "v.io/x/ref/profiles"
	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/profiles/internal/rpc/stream"
	"v.io/x/ref/profiles/internal/rpc/stream/manager"
	"v.io/x/ref/profiles/internal/rpc/stream/proxy"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
	"v.io/x/ref/profiles/internal/rpc/stream/vif"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

//go:generate v23 test generate

func TestProxy(t *testing.T) {
	ctx, shutdown := v23Init()
	defer shutdown()

	_, shutdown, proxyEp, err := proxy.InternalNew(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), ctx, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	principal := testutil.NewPrincipal("test")
	blessings := principal.BlessingStore().Default()

	// Create the stream.Manager for the server.
	server1 := manager.InternalNew(naming.FixedRoutingID(0x1111111111111111))
	defer server1.Shutdown()
	// Setup a stream.Listener that will accept VCs and Flows routed
	// through the proxy.
	ln1, ep1, err := server1.Listen(proxyEp.Network(), proxyEp.String(), principal, blessings)
	if err != nil {
		t.Logf(verror.DebugString(err))
		t.Fatal(err)
	}
	defer ln1.Close()

	// Create the stream.Manager for a second server.
	server2 := manager.InternalNew(naming.FixedRoutingID(0x2222222222222222))
	defer server2.Shutdown()
	// Setup a stream.Listener that will accept VCs and Flows routed
	// through the proxy.
	ln2, ep2, err := server2.Listen(proxyEp.Network(), proxyEp.String(), principal, blessings)
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()

	// Create the stream.Manager for a client.
	client := manager.InternalNew(naming.FixedRoutingID(0xcccccccccccccccc))
	defer client.Shutdown()

	cases := []struct {
		client stream.Manager
		ln     stream.Listener
		ep     naming.Endpoint
	}{
		{client, ln1, ep1},  // client writing to server1
		{server1, ln2, ep2}, // server1 writing to server2
		{server1, ln1, ep1}, // server1 writing to itself
	}

	const written = "the dough rises"
	for i, c := range cases {
		name := fmt.Sprintf("case #%d(write to %v):", i, c.ep)
		// Accept a single flow and write out what is read to readChan
		readChan := make(chan string)
		go readFlow(t, c.ln, readChan)
		if err := writeFlow(c.client, c.ep, written); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		// Validate that the data read is the same as the data written.
		if read := <-readChan; read != written {
			t.Errorf("case #%d: Read %q, wrote %q", i, read, written)
		}
	}
}

func TestProxyAuthorization(t *testing.T) {
	ctx, shutdown := v23Init()
	defer shutdown()

	_, shutdown, proxyEp, err := proxy.InternalNew(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), ctx, testAuth{"alice", "carol"})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()

	var (
		alice = testutil.NewPrincipal("alice")
		bob   = testutil.NewPrincipal("bob")
		carol = testutil.NewPrincipal("carol")
		dave  = testutil.NewPrincipal("dave")
	)
	// Make the proxy recognize "alice", "bob" and "carol", but not "dave"
	v23.GetPrincipal(ctx).AddToRoots(alice.BlessingStore().Default())
	v23.GetPrincipal(ctx).AddToRoots(bob.BlessingStore().Default())
	v23.GetPrincipal(ctx).AddToRoots(carol.BlessingStore().Default())

	testcases := []struct {
		p  security.Principal
		ok bool
	}{
		{alice, true}, // passes the auth policy
		{bob, false},  // recognized, but not included in auth policy
		{carol, true}, // passes the auth policy
		{dave, false}, // not recognized, thus doesn't pass the auth policy
	}
	for idx, test := range testcases {
		server := manager.InternalNew(naming.FixedRoutingID(uint64(idx)))
		_, ep, err := server.Listen(proxyEp.Network(), proxyEp.String(), test.p, test.p.BlessingStore().Default(), proxyAuth{test.p})
		if (err == nil) != test.ok {
			t.Errorf("Got ep=%v, err=%v - wanted error:%v", ep, err, !test.ok)
		}
		server.Shutdown()
	}
}

type proxyAuth struct {
	p security.Principal
}

func (proxyAuth) RPCStreamListenerOpt() {}
func (a proxyAuth) Login(stream.Flow) (security.Blessings, []security.Discharge, error) {
	return a.p.BlessingStore().Default(), nil, nil
}

func TestDuplicateRoutingID(t *testing.T) {
	ctx, shutdown := v23Init()
	defer shutdown()

	_, shutdown, proxyEp, err := proxy.InternalNew(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), ctx, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()

	// Create the stream.Manager for server1 and server2, both with the same routing ID
	serverRID := naming.FixedRoutingID(0x5555555555555555)
	server1 := manager.InternalNew(serverRID)
	server2 := manager.InternalNew(serverRID)
	defer server1.Shutdown()
	defer server2.Shutdown()

	principal := testutil.NewPrincipal("test")
	blessings := principal.BlessingStore().Default()

	// First server to claim serverRID should win.
	ln1, ep1, err := server1.Listen(proxyEp.Network(), proxyEp.String(), principal, blessings)
	if err != nil {
		t.Fatal(err)
	}
	defer ln1.Close()

	ln2, ep2, err := server2.Listen(proxyEp.Network(), proxyEp.String(), principal, blessings)
	if pattern := "routing id 00000000000000005555555555555555 is already being proxied"; err == nil || !strings.Contains(err.Error(), pattern) {
		t.Errorf("Got (%v, %v, %v) want error \"...%v\" (ep1:%v)", ln2, ep2, err, pattern, ep1)
	}
}

func TestProxyAuthentication(t *testing.T) {
	ctx, shutdown := v23Init()
	defer shutdown()

	pproxy := v23.GetPrincipal(ctx)
	_, shutdown, proxyEp, err := proxy.InternalNew(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), ctx, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	if got, want := proxyEp.BlessingNames(), []string{"proxy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Proxy endpoint blessing names: got %v, want %v", got, want)
	}

	other := manager.InternalNew(naming.FixedRoutingID(0xcccccccccccccccc))
	defer other.Shutdown()

	vc, err := other.Dial(proxyEp, testutil.NewPrincipal("other"))
	if err != nil {
		t.Fatal(err)
	}

	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := flow.RemoteBlessings(), pproxy.BlessingStore().Default(); !reflect.DeepEqual(got, want) {
		t.Errorf("Proxy authenticated as [%v], want [%v]", got, want)
	}
}

func TestServerBlessings(t *testing.T) {
	ctx, shutdown := v23Init()
	defer shutdown()

	var (
		pserver = testutil.NewPrincipal("server")
		pclient = testutil.NewPrincipal("client")
	)

	_, shutdown, proxyEp, err := proxy.InternalNew(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), ctx, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	if got, want := proxyEp.BlessingNames(), []string{"proxy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Proxy endpoint blessing names: got %v, want %v", got, want)
	}

	server := manager.InternalNew(naming.FixedRoutingID(0x5555555555555555))
	defer server.Shutdown()

	ln, ep, err := server.Listen(proxyEp.Network(), proxyEp.String(), pserver, pserver.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ep.BlessingNames(), []string{"server"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Server endpoint %q: Got BlessingNames %v, want %v", ep, got, want)
	}
	defer ln.Close()
	go func() {
		for {
			if _, err := ln.Accept(); err != nil {
				return
			}
		}
	}()

	client := manager.InternalNew(naming.FixedRoutingID(0xcccccccccccccccc))
	defer client.Shutdown()
	vc, err := client.Dial(ep, pclient)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := flow.RemoteBlessings(), pserver.BlessingStore().Default(); !reflect.DeepEqual(got, want) {
		t.Errorf("Got [%v] want [%v]", got, want)
	}
}

func TestHostPort(t *testing.T) {
	ctx, shutdown := v23Init()
	defer shutdown()

	_, shutdown, proxyEp, err := proxy.InternalNew(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), ctx, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	server := manager.InternalNew(naming.FixedRoutingID(0x5555555555555555))
	defer server.Shutdown()
	addr := proxyEp.Addr().String()
	port := addr[strings.LastIndex(addr, ":"):]
	principal := testutil.NewPrincipal("test")
	blessings := principal.BlessingStore().Default()
	ln, _, err := server.Listen(inaming.Network, "127.0.0.1"+port, principal, blessings)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

func TestClientBecomesServer(t *testing.T) {
	ctx, shutdown := v23Init()
	defer shutdown()

	_, shutdown, proxyEp, err := proxy.InternalNew(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), ctx, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	server := manager.InternalNew(naming.FixedRoutingID(0x5555555555555555))
	client1 := manager.InternalNew(naming.FixedRoutingID(0x1111111111111111))
	client2 := manager.InternalNew(naming.FixedRoutingID(0x2222222222222222))
	defer shutdown()
	defer server.Shutdown()
	defer client1.Shutdown()
	defer client2.Shutdown()

	principal := testutil.NewPrincipal("test")
	blessings := principal.BlessingStore().Default()
	lnS, epS, err := server.Listen(proxyEp.Network(), proxyEp.String(), principal, blessings)
	if err != nil {
		t.Fatal(err)
	}
	defer lnS.Close()
	rchan := make(chan string)

	pclient1 := testutil.NewPrincipal("client1")

	// client1 must connect to the proxy to speak to the server.
	// Keep a VC and Flow open to the server, to ensure that the proxy
	// maintains routing information (at some point, inactive VIFs
	// should be garbage collected, so this ensures that the VIF
	// is "active")
	if vc, err := client1.Dial(epS, pclient1); err != nil {
		t.Fatal(err)
	} else if flow, err := vc.Connect(); err != nil {
		t.Fatal(err)
	} else {
		defer flow.Close()
	}

	// Now client1 becomes a server
	lnC, epC, err := client1.Listen(proxyEp.Network(), proxyEp.String(), pclient1, pclient1.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}
	defer lnC.Close()
	// client2 should be able to talk to client1 through the proxy
	rchan = make(chan string)
	go readFlow(t, lnC, rchan)
	if err := writeFlow(client2, epC, "daffy duck"); err != nil {
		t.Fatalf("client2 failed to chat with client1: %v", err)
	}
	if got, want := <-rchan, "daffy duck"; got != want {
		t.Fatalf("client2->client1 got %q want %q", got, want)
	}
}

func testProxyIdleTimeout(t *testing.T, testServer bool) {
	ctx, shutdown := v23Init()
	defer shutdown()

	const (
		idleTime = 10 * time.Millisecond
		// We use a long wait time here since it takes some time to handle VC close
		// especially in race testing.
		waitTime = 150 * time.Millisecond
	)

	var (
		pserver = testutil.NewPrincipal("server")
		pclient = testutil.NewPrincipal("client")

		opts  []stream.VCOpt
		lopts []stream.ListenerOpt
	)
	if testServer {
		lopts = []stream.ListenerOpt{vc.IdleTimeout{idleTime}}
	} else {
		opts = []stream.VCOpt{vc.IdleTimeout{idleTime}}
	}

	// Pause the idle timers.
	triggerTimers := vif.SetFakeTimers()

	Proxy, shutdown, proxyEp, err := proxy.InternalNew(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), ctx, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()

	// Create the stream.Manager for the server.
	server := manager.InternalNew(naming.FixedRoutingID(0x1111111111111111))
	defer server.Shutdown()
	// Setup a stream.Listener that will accept VCs and Flows routed
	// through the proxy.
	ln, ep, err := server.Listen(proxyEp.Network(), proxyEp.String(), pserver, pserver.BlessingStore().Default(), lopts...)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			if _, err := ln.Accept(); err != nil {
				return
			}
		}
	}()

	// Create the stream.Manager for a client.
	client := manager.InternalNew(naming.FixedRoutingID(0xcccccccccccccccc))
	defer client.Shutdown()

	// Open a VC and a Flow.
	VC, err := client.Dial(ep, pclient, opts...)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := VC.Connect()
	if err != nil {
		t.Fatal(err)
	}

	// Trigger the idle timers.
	triggerTimers()

	if numProcs := proxy.NumProcesses(Proxy); numProcs != 2 {
		// There should be two processes at this point.
		t.Fatal(fmt.Errorf("Unexpected number of processes: %d\n", numProcs))
	}

	// There is one active flow. The VC should be kept open.
	time.Sleep(waitTime)
	if numProcs := proxy.NumProcesses(Proxy); numProcs != 2 {
		t.Errorf("Want VC is kept open; closed")
	}

	flow.Close()

	// The flow has been closed. The VC should be closed after idle timeout.
	for range time.Tick(idleTime) {
		if proxy.NumProcesses(Proxy) == 1 {
			break
		}
	}

	client.ShutdownEndpoint(ep)

	// Even when the idle timeout is set for VC in server, we still should be
	// able to dial to the server through the proxy, since one VC between the
	// server and the proxy should be kept alive as the proxy protocol.
	//
	// We use fake timers here again to avoid idle timeout during dialing.
	defer vif.SetFakeTimers()()
	if _, err := client.Dial(ep, pclient, opts...); err != nil {
		t.Errorf("Want to dial to the server; can't dial: %v", err)
	}
}

func TestProxyIdleTimeout(t *testing.T)       { testProxyIdleTimeout(t, false) }
func TestProxyIdleTimeoutServer(t *testing.T) { testProxyIdleTimeout(t, true) }

func writeFlow(mgr stream.Manager, ep naming.Endpoint, data string) error {
	vc, err := mgr.Dial(ep, testutil.NewPrincipal("test"))
	if err != nil {
		return fmt.Errorf("manager.Dial(%v) failed: %v", ep, err)
	}
	flow, err := vc.Connect()
	if err != nil {
		return fmt.Errorf("vc.Connect failed: %v", err)
	}
	defer flow.Close()
	if _, err := flow.Write([]byte(data)); err != nil {
		return fmt.Errorf("flow.Write failed: %v", err)
	}
	return nil
}

func readFlow(t *testing.T, ln stream.Listener, read chan<- string) {
	defer close(read)
	flow, err := ln.Accept()
	if err != nil {
		t.Error(err)
		return
	}
	var tmp [1024]byte
	var buf bytes.Buffer
	for {
		n, err := flow.Read(tmp[:])
		if err == io.EOF {
			read <- buf.String()
			return
		}
		if err != nil {
			t.Error(err)
			return
		}
		buf.Write(tmp[:n])
	}
}

func v23Init() (*context.T, func()) {
	ctx, shutdown := test.InitForTest()
	ctx, err := v23.WithPrincipal(ctx, testutil.NewPrincipal("proxy"))
	if err != nil {
		panic(err)
	}
	return ctx, shutdown
}

type testAuth []string

func (l testAuth) Authorize(ctx *context.T, call security.Call) error {
	remote, rejected := security.RemoteBlessingNames(ctx, call)
	for _, n := range remote {
		for _, a := range l {
			if n == a {
				return nil
			}
		}
	}
	return fmt.Errorf("%v not in authorized set of %v (rejected: %v)", remote, l, rejected)
}
