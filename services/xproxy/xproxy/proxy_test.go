// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxy_test

import (
	"bufio"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"v.io/x/ref"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/xproxy/xproxy"
	"v.io/x/ref/test"
	"v.io/x/ref/test/goroutines"
	"v.io/x/ref/test/testutil"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

const (
	leakWaitTime = 250 * time.Millisecond
	pollTime     = 50 * time.Millisecond
	bidiProtocol = "bidi"
)

type testService struct{}

func (t *testService) Echo(ctx *context.T, call rpc.ServerCall, arg string) (string, error) {
	return "response:" + arg, nil
}

func TestProxyRPC(t *testing.T) {
	if ref.RPCTransitionState() != ref.XServers {
		t.Skip("Test only runs under 'V23_RPC_TRANSITION_STATE==xservers'")
	}
	defer goroutines.NoLeaks(t, leakWaitTime)()
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	// Start the proxy.
	pname, stop := startProxy(t, ctx, "proxy", "", address{"tcp", "127.0.0.1:0"})
	defer stop()

	// Start the server listening through the proxy.
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Proxy: pname})
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, _, err := v23.WithNewServer(sctx, "server", &testService{}, nil); err != nil {
		t.Fatal(err)
	}

	var got string
	if err := v23.GetClient(ctx).Call(ctx, "server", "Echo", []interface{}{"hello"}, []interface{}{&got}); err != nil {
		t.Fatal(err)
	}
	if want := "response:hello"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMultipleProxyRPC(t *testing.T) {
	if ref.RPCTransitionState() != ref.XServers {
		t.Skip("Test only runs under 'V23_RPC_TRANSITION_STATE==xservers'")
	}
	defer goroutines.NoLeaks(t, leakWaitTime)()
	kp := newKillProtocol()
	flow.RegisterProtocol("kill", kp)
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	// Start the proxies.
	p1name, stop := startProxy(t, ctx, "p1", "", address{"kill", "127.0.0.1:0"})
	defer stop()
	p2name, stop := startProxy(t, ctx, "p2", p1name, address{"kill", "127.0.0.1:0"})
	defer stop()

	// Start the server listening through the proxy.
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Proxy: p2name})
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, _, err := v23.WithNewServer(sctx, "server", &testService{}, nil); err != nil {
		t.Fatal(err)
	}

	var got string
	if err := v23.GetClient(ctx).Call(ctx, "server", "Echo", []interface{}{"hello"}, []interface{}{&got}); err != nil {
		t.Fatal(err)
	}
	if want := "response:hello"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestProxyNotAuthorized(t *testing.T) {
	if ref.RPCTransitionState() != ref.XServers {
		t.Skip("Test only runs under 'V23_RPC_TRANSITION_STATE==xservers'")
	}
	defer goroutines.NoLeaks(t, leakWaitTime)()
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	// Make principals for the proxy, server, and client.
	pctx, err := v23.WithPrincipal(ctx, testutil.NewPrincipal("proxy"))
	if err != nil {
		t.Fatal(err)
	}
	sctx, err := v23.WithPrincipal(ctx, testutil.NewPrincipal("server"))
	if err != nil {
		t.Fatal(err)
	}
	cctx, err := v23.WithPrincipal(ctx, testutil.NewPrincipal("client"))
	if err != nil {
		t.Fatal(err)
	}

	// Have the root bless the client, server, and proxy.
	root := testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx))
	if err := root.Bless(v23.GetPrincipal(pctx), "proxy"); err != nil {
		t.Fatal(err)
	}
	if err := root.Bless(v23.GetPrincipal(cctx), "client"); err != nil {
		t.Fatal(err)
	}
	if err := root.Bless(v23.GetPrincipal(sctx), "server"); err != nil {
		t.Fatal(err)
	}

	// Now the proxy's blessings would fail authorization from the client using the
	// default authorizer.
	pname, stop := startProxy(t, pctx, "proxy", "", address{"tcp", "127.0.0.1:0"})
	defer stop()

	// Start the server listening through the proxy.
	sctx = v23.WithListenSpec(sctx, rpc.ListenSpec{Proxy: pname})
	sctx, cancel := context.WithCancel(sctx)
	defer cancel()
	if _, _, err := v23.WithNewServer(sctx, "server", &testService{}, security.AllowEveryone()); err != nil {
		t.Fatal(err)
	}
	// The call should succeed which means that the client did not try to authorize
	// the proxy's blessings.
	var got string
	if err := v23.GetClient(cctx).Call(cctx, "server", "Echo", []interface{}{"hello"},
		[]interface{}{&got}, options.ServerAuthorizer{rejectProxyAuthorizer{}}); err != nil {
		t.Fatal(err)
	}
	if want := "response:hello"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

type rejectProxyAuthorizer struct{}

func (rejectProxyAuthorizer) Authorize(ctx *context.T, call security.Call) error {
	names, _ := security.RemoteBlessingNames(ctx, call)
	for _, n := range names {
		if strings.Contains(n, "proxy") {
			panic("should not call authorizer on proxy")
		}
	}
	return nil
}

// TODO(suharshs): Remove the below tests when the transition is complete.
func TestSingleProxy(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	kp := newKillProtocol()
	flow.RegisterProtocol("kill", kp)
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()
	am, err := v23.NewFlowManager(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := v23.NewFlowManager(ctx)
	if err != nil {
		t.Fatal(err)
	}

	pname, stop := startProxy(t, ctx, "", "", address{"kill", "127.0.0.1:0"})
	defer stop()
	address, _ := naming.SplitAddressName(pname)
	pep, err := v23.NewEndpoint(address)
	if err != nil {
		t.Fatal(err)
	}

	if err := am.ProxyListen(ctx, pep); err != nil {
		t.Fatal(err)
	}
	for {
		eps, changed := am.ListeningEndpoints()
		if eps[0].Addr().Network() != bidiProtocol {
			if err := testEndToEndConnection(t, ctx, dm, am, eps[0]); err != nil {
				t.Error(err)
			}
			return
		}
		<-changed
	}
}

func TestMultipleProxies(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	kp := newKillProtocol()
	flow.RegisterProtocol("kill", kp)
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()
	am, err := v23.NewFlowManager(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := v23.NewFlowManager(ctx)
	if err != nil {
		t.Fatal(err)
	}

	p1name, stop := startProxy(t, ctx, "", "", address{"kill", "127.0.0.1:0"})
	defer stop()
	p2name, stop := startProxy(t, ctx, "", p1name, address{"kill", "127.0.0.1:0"})
	defer stop()

	p3name, stop := startProxy(t, ctx, "", p2name, address{"kill", "127.0.0.1:0"})
	defer stop()
	address, _ := naming.SplitAddressName(p3name)
	p3ep, err := v23.NewEndpoint(address)
	if err != nil {
		t.Fatal(err)
	}

	if err := am.ProxyListen(ctx, p3ep); err != nil {
		t.Fatal(err)
	}

	for {
		eps, changed := am.ListeningEndpoints()
		// TODO(suharshs): Fix this test once we have the proxy send update messages to the
		// server when it reconnects to a proxy. This test only really tests the first connection
		// currently because the connections are cached. So we need to kill connections and
		// wait for them to reestablish but we need proxies to update communicate their new endpoints
		// to each other and to the server. For now we at least check a random endpoint so the
		// test will at least fail over many runs if something is wrong.
		if eps[0].Addr().Network() != bidiProtocol {
			if err := testEndToEndConnection(t, ctx, dm, am, eps[rand.Int()%3]); err != nil {
				t.Error(err)
			}
			return
		}
		<-changed
	}
}

func testEndToEndConnection(t *testing.T, ctx *context.T, dm, am flow.Manager, aep naming.Endpoint) error {
	// The dialing flow.Manager dials a flow to the accepting flow.Manager.
	want := "Do you read me?"
	df, err := dm.Dial(ctx, aep, allowAllPeersAuthorizer{})
	if err != nil {
		return err
	}
	// We write before accepting to ensure that the openFlow message is sent.
	if err := writeLine(df, want); err != nil {
		return err
	}
	af, err := am.Accept(ctx)
	if err != nil {
		return err
	}
	got, err := readLine(af)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("got %v, want %v", got, want)
	}

	// Writes in the opposite direction should work as well.
	want = "I read you loud and clear."
	if err := writeLine(af, want); err != nil {
		return err
	}
	got, err = readLine(df)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("got %v, want %v", got, want)
	}
	return nil
}

// TODO(suharshs): Add test for bidirectional RPC.

func readLine(f flow.Flow) (string, error) {
	s, err := bufio.NewReader(f).ReadString('\n')
	return strings.TrimRight(s, "\n"), err
}

func writeLine(f flow.Flow, data string) error {
	data += "\n"
	_, err := f.Write([]byte(data))
	return err
}

type allowAllPeersAuthorizer struct{}

func (allowAllPeersAuthorizer) AuthorizePeer(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) ([]string, []security.RejectedBlessing, error) {
	return nil, nil, nil
}

func (allowAllPeersAuthorizer) BlessingsForPeer(ctx *context.T, _ []string) (
	security.Blessings, map[string]security.Discharge, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
}

type address struct {
	Protocol, Address string
}

func startProxy(t *testing.T, ctx *context.T, name string, listenOnProxy string, addrs ...address) (string, func()) {
	var ls rpc.ListenSpec
	hasProxies := len(listenOnProxy) > 0
	for _, addr := range addrs {
		ls.Addrs = append(ls.Addrs, addr)
	}
	ls.Proxy = listenOnProxy
	ctx = v23.WithListenSpec(ctx, ls)
	ctx, cancel := context.WithCancel(ctx)
	proxy, err := xproxy.New(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	stop := func() {
		cancel()
		<-proxy.Closed()
	}
	// Wait for the proxy to connect to its proxies.
	if hasProxies {
		for len(proxy.MultipleProxyEndpoints()) == 0 {
			time.Sleep(pollTime)
		}
	}
	if len(name) > 0 {
		waitForPublish(ctx, name)
		return name, stop
	}
	peps := proxy.ListeningEndpoints()
	for _, pep := range peps {
		if pep.Addr().Network() == "tcp" || pep.Addr().Network() == "kill" {
			return pep.Name(), stop
		}
	}
	t.Fatal("Proxy not listening on network address.")
	return "", nil
}

func waitForPublish(ctx *context.T, name string) {
	ns := v23.GetNamespace(ctx)
	for {
		if entry, err := ns.Resolve(ctx, name); err == nil && len(entry.Names()) > 0 {
			return
		}
		time.Sleep(pollTime)
	}
}

type killProtocol struct {
	protocol flow.Protocol
	mu       sync.Mutex
	conns    []flow.Conn
}

func newKillProtocol() *killProtocol {
	p, _ := flow.RegisteredProtocol("tcp")
	return &killProtocol{protocol: p}
}

func (p *killProtocol) KillConnections() {
	p.mu.Lock()
	for _, c := range p.conns {
		c.Close()
	}
	p.conns = nil
	p.mu.Unlock()
}

func (p *killProtocol) Dial(ctx *context.T, protocol, address string, timeout time.Duration) (flow.Conn, error) {
	c, err := p.protocol.Dial(ctx, "tcp", address, timeout)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.conns = append(p.conns, c)
	p.mu.Unlock()
	return c, nil
}

func (p *killProtocol) Listen(ctx *context.T, protocol, address string) (flow.Listener, error) {
	return p.protocol.Listen(ctx, "tcp", address)
}

func (p *killProtocol) Resolve(ctx *context.T, protocol, address string) (string, string, error) {
	return p.protocol.Resolve(ctx, "tcp", address)
}
