// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxyd_test

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
	"v.io/x/ref/services/xproxyd"
	"v.io/x/ref/test/goroutines"
	"v.io/x/ref/test/testutil"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
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
	ctx, shutdown := v23.Init()
	defer shutdown()

	// Start the proxy.
	pep := startProxy(t, ctx, address{"tcp", "127.0.0.1:0"})

	// Start the server listening through the proxy.
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Proxy: pep.Name()})
	_, s, err := v23.WithNewServer(ctx, "", &testService{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the server to finish listening through the proxy.
	eps := s.Status().Endpoints
	for ; len(eps) == 0 || eps[0].Addr().Network() == bidiProtocol; eps = s.Status().Endpoints {
		time.Sleep(pollTime)
	}

	var got string
	if err := v23.GetClient(ctx).Call(ctx, eps[0].Name(), "Echo", []interface{}{"hello"}, []interface{}{&got}); err != nil {
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
	ctx, shutdown := v23.Init()
	defer shutdown()

	// Start the proxies.
	pep := startProxy(t, ctx, address{"kill", "127.0.0.1:0"})
	p2ep := startProxy(t, ctx, address{"v23", pep.String()}, address{"kill", "127.0.0.1:0"})

	// Start the server listening through the proxy.
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Proxy: p2ep.Name()})
	_, s, err := v23.WithNewServer(ctx, "", &testService{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the server to finish listening through the proxy.
	eps := s.Status().Endpoints
	for ; len(eps) == 0 || eps[0].Addr().Network() == bidiProtocol; eps = s.Status().Endpoints {
		time.Sleep(pollTime)
	}

	var got string
	if err := v23.GetClient(ctx).Call(ctx, eps[0].Name(), "Echo", []interface{}{"hello"}, []interface{}{&got}); err != nil {
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
	ctx, shutdown := v23.Init()
	defer shutdown()

	// Make principals for the proxy and server.
	pctx, err := v23.WithPrincipal(ctx, testutil.NewPrincipal("proxy"))
	if err != nil {
		t.Fatal(err)
	}
	sctx, err := v23.WithPrincipal(ctx, testutil.NewPrincipal("server"))
	if err != nil {
		t.Fatal(err)
	}

	// Server blesses the proxy so that the server is willing to talk to it.
	ids := testutil.IDProviderFromPrincipal(v23.GetPrincipal(sctx))
	if err := ids.Bless(v23.GetPrincipal(pctx), "proxy"); err != nil {
		t.Fatal(err)
	}
	// Client blesses the server so that the client is willing to talk to it.
	idc := testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx))
	if err := idc.Bless(v23.GetPrincipal(sctx), "server"); err != nil {
		t.Fatal(err)
	}

	// Now the proxy's blessings would fail authorization from the client using the
	// default authorizer.
	pep := startProxy(t, pctx, address{"tcp", "127.0.0.1:0"})

	// Start the server listening through the proxy.
	sctx = v23.WithListenSpec(sctx, rpc.ListenSpec{Proxy: pep.Name()})
	_, s, err := v23.WithNewServer(sctx, "", &testService{}, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the server to finish listening through the proxy.
	eps := s.Status().Endpoints
	for ; len(eps) == 0 || eps[0].Addr().Network() == bidiProtocol; eps = s.Status().Endpoints {
		time.Sleep(pollTime)
	}
	// The call should succeed which means that the client did not try to authorize
	// the proxy's blessings.
	var got string
	if err := v23.GetClient(ctx).Call(ctx, eps[0].Name(), "Echo", []interface{}{"hello"}, []interface{}{&got}); err != nil {
		t.Fatal(err)
	}
	if want := "response:hello"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TODO(suharshs): Remove the below tests when the transition is complete.
func TestSingleProxy(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	kp := newKillProtocol()
	flow.RegisterProtocol("kill", kp)
	ctx, shutdown := v23.Init()
	defer shutdown()
	am, err := v23.NewFlowManager(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := v23.NewFlowManager(ctx)
	if err != nil {
		t.Fatal(err)
	}

	pep := startProxy(t, ctx, address{"kill", "127.0.0.1:0"})

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
	ctx, shutdown := v23.Init()
	defer shutdown()
	am, err := v23.NewFlowManager(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := v23.NewFlowManager(ctx)
	if err != nil {
		t.Fatal(err)
	}

	pep := startProxy(t, ctx, address{"kill", "127.0.0.1:0"})

	p2ep := startProxy(t, ctx, address{"v23", pep.String()}, address{"kill", "127.0.0.1:0"})

	p3ep := startProxy(t, ctx, address{"v23", p2ep.String()}, address{"kill", "127.0.0.1:0"})

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

func startProxy(t *testing.T, ctx *context.T, addrs ...address) naming.Endpoint {
	var ls rpc.ListenSpec
	hasProxies := false
	for _, addr := range addrs {
		ls.Addrs = append(ls.Addrs, addr)
		if addr.Protocol == "v23" {
			hasProxies = true
		}
	}
	ctx = v23.WithListenSpec(ctx, ls)
	proxy, _, err := xproxyd.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the proxy to connect to its proxies.
	if hasProxies {
		for len(proxy.MultipleProxyEndpoints()) == 0 {
			time.Sleep(pollTime)
		}
	}
	peps := proxy.ListeningEndpoints()
	for _, pep := range peps {
		if pep.Addr().Network() == "tcp" || pep.Addr().Network() == "kill" {
			return pep
		}
	}
	t.Fatal("Proxy not listening on network address.")
	return nil
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
