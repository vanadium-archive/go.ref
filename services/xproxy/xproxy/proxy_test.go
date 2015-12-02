// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxy_test

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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
	bidiProtocol = "bidi"
)

var randData []byte

func init() {
	randData = make([]byte, 1<<17)
	if _, err := rand.Read(randData); err != nil {
		panic("Could not read random data.")
	}
}

type testService struct{}

func (t *testService) Echo(ctx *context.T, call rpc.ServerCall, arg string) (string, error) {
	return "response:" + arg, nil
}

func TestBigProxyRPC(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	ctx, shutdown := test.V23Init()
	defer shutdown()

	// Start the proxy.
	pname, stop := startProxy(t, ctx, "", security.AllowEveryone(), "", address{"tcp", "127.0.0.1:0"})
	defer stop()
	// Start the server listening through the proxy.
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Proxy: pname})
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, server, err := v23.WithNewServer(sctx, "", &testService{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	for {
		status := server.Status()
		if status.Endpoints[0].Addr().Network() != bidiProtocol {
			name = status.Endpoints[0].Name()
			break
		}
		<-status.Valid
	}
	var got string
	if err := v23.GetClient(ctx).Call(ctx, name, "Echo", []interface{}{string(randData)}, []interface{}{&got}); err != nil {
		t.Fatal(err)
	}
	if want := "response:" + string(randData); got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestProxyRPC(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	// Start the proxy.
	pname, stop := startProxy(t, ctx, "proxy", security.AllowEveryone(), "", address{"tcp", "127.0.0.1:0"})
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
	defer goroutines.NoLeaks(t, leakWaitTime)()
	kp := newKillProtocol()
	flow.RegisterProtocol("kill", kp)
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	// Start the proxies.
	p1name, stop := startProxy(t, ctx, "p1", security.AllowEveryone(), "", address{"kill", "127.0.0.1:0"})
	defer stop()
	p2name, stop := startProxy(t, ctx, "p2", security.AllowEveryone(), p1name, address{"kill", "127.0.0.1:0"})
	defer stop()

	// Start the server listening through the proxy.
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Proxy: p2name})
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, _, err := v23.WithNewServer(sctx, "server", &testService{}, nil); err != nil {
		t.Error(err)
	}

	var got string
	if err := v23.GetClient(ctx).Call(ctx, "server", "Echo", []interface{}{"hello"}, []interface{}{&got}); err != nil {
		t.Error(err)
	}
	if want := "response:hello"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	kp.KillConnections()
	// Killing the connections and trying again should work.
	for {
		if err := v23.GetClient(ctx).Call(ctx, "server", "Echo", []interface{}{"hello"}, []interface{}{&got}); err == nil {
			break
		}
	}
	if want := "response:hello"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestProxyNotAuthorized(t *testing.T) {
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
	pname, stop := startProxy(t, pctx, "proxy", security.AllowEveryone(), "", address{"tcp", "127.0.0.1:0"})
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

func TestProxyAuthorizesServer(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
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

	// Have the root bless the proxy and server.
	root := testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx))
	if err := root.Bless(v23.GetPrincipal(pctx), "proxy"); err != nil {
		t.Fatal(err)
	}
	if err := root.Bless(v23.GetPrincipal(sctx), "server"); err != nil {
		t.Fatal(err)
	}

	// Start a proxy that accepts connections from everyone and ensure that it does.
	pname, stop := startProxy(t, pctx, "acceptproxy", security.AllowEveryone(), "", address{"tcp", "127.0.0.1:0"})
	defer stop()

	sctx = v23.WithListenSpec(sctx, rpc.ListenSpec{Proxy: pname})
	sctx, cancel := context.WithCancel(sctx)
	defer cancel()
	_, server, err := v23.WithNewServer(sctx, "", &testService{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var proxyEP naming.Endpoint
	for {
		status := server.Status()
		if status.Endpoints[0].Addr().Network() != bidiProtocol {
			proxyEP = status.Endpoints[0]
			break
		}
		<-status.Valid
	}

	// A proxy using the default authorizer should not authorize the server.
	pname, stop = startProxy(t, pctx, "acceptproxy", nil, "", address{"tcp", "127.0.0.1:0"})
	defer stop()

	_, server, err = v23.WithNewServer(sctx, "", &testService{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// TODO(suharshs): Find a way to test this without having to have this time.Sleep.
	// Perhaps the manager can return errors about why connecting to the proxy failed.
	time.Sleep(time.Second)
	if server.Status().Endpoints[0].Addr().Network() != bidiProtocol {
		t.Errorf("proxy should have rejected the server's request to listen through it")
	}

	// Artificially constructing the proxied endpoint to the server should
	// not work. (i.e. a client cannot "trick" a proxy into connecting to a server
	// that the proxy doesn't want to talk to).
	ep, err := setEndpointRoutingID(proxyEP, server.Status().Endpoints[0].RoutingID())
	if err != nil {
		t.Error(err)
	}
	var got string
	if err := v23.GetClient(ctx).Call(ctx, ep.Name(), "Echo", []interface{}{"hello"}, []interface{}{&got}, options.NoRetry{}); err == nil {
		t.Error("proxy should not have authorized server.")
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
	ctx, shutdown := test.V23Init()
	am, err := v23.NewFlowManager(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := v23.NewFlowManager(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		<-am.Closed()
		<-dm.Closed()
	}()
	defer shutdown()

	pname, stop := startProxy(t, ctx, "", security.AllowEveryone(), "", address{"kill", "127.0.0.1:0"})
	defer stop()
	address, _ := naming.SplitAddressName(pname)
	pep, err := v23.NewEndpoint(address)
	if err != nil {
		t.Fatal(err)
	}

	go am.ProxyListen(ctx, pep)

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
	ctx, shutdown := test.V23Init()
	am, err := v23.NewFlowManager(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := v23.NewFlowManager(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		<-dm.Closed()
		<-am.Closed()
	}()

	p1name, stop := startProxy(t, ctx, "", security.AllowEveryone(), "", address{"kill", "127.0.0.1:0"})
	defer stop()
	p2name, stop := startProxy(t, ctx, "", security.AllowEveryone(), p1name, address{"kill", "127.0.0.1:0"})
	defer stop()

	p3name, stop := startProxy(t, ctx, "", security.AllowEveryone(), p2name, address{"kill", "127.0.0.1:0"})
	defer stop()
	address, _ := naming.SplitAddressName(p3name)
	p3ep, err := v23.NewEndpoint(address)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		// We are going to kill connections 3 times so we need to reconnect to the proxy.
		for i := 0; i < 3; i++ {
			am.ProxyListen(ctx, p3ep)
		}
	}()

	for i := 0; i < 3; {
		eps, changed := am.ListeningEndpoints()
		if eps[0].Addr().Network() != bidiProtocol && len(eps) > i {
			if err := testEndToEndConnection(t, ctx, dm, am, eps[i]); err != nil {
				// TODO(suharshs): We sometimes begin dialing the connection before the
				// conn has been closed. In that case, we will get an error when
				// writing on the connection. For that reason, we don't error out if we
				// get an error here, and instead keep trying until we succeed.
				continue
			}
			i++
			kp.KillConnections()
		}
		<-changed
	}
	shutdown()
}

func testEndToEndConnection(t *testing.T, ctx *context.T, dm, am flow.Manager, aep naming.Endpoint) error {
	// The dialing flow.Manager dials a flow to the accepting flow.Manager.
	want := "Do you read me?"
	df, err := dm.Dial(ctx, aep, allowAllPeersAuthorizer{}, 0)
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

func startProxy(t *testing.T, ctx *context.T, name string, auth security.Authorizer, listenOnProxy string, addrs ...address) (string, func()) {
	var ls rpc.ListenSpec
	for _, addr := range addrs {
		ls.Addrs = append(ls.Addrs, addr)
	}
	ls.Proxy = listenOnProxy
	ctx = v23.WithListenSpec(ctx, ls)
	ctx, cancel := context.WithCancel(ctx)
	proxy, err := xproxy.New(ctx, name, auth)
	if err != nil {
		t.Fatal(err)
	}
	stop := func() {
		cancel()
		<-proxy.Closed()
	}
	if len(name) > 0 {
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

type killProtocol struct {
	protocol flow.Protocol
	mu       sync.Mutex
	conns    []flow.Conn
}

type kpListener struct {
	kp *killProtocol
	flow.Listener
}

func (l *kpListener) Accept(ctx *context.T) (flow.Conn, error) {
	c, err := l.Listener.Accept(ctx)
	if err != nil {
		return nil, err
	}
	l.kp.mu.Lock()
	l.kp.conns = append(l.kp.conns, c)
	l.kp.mu.Unlock()
	return c, err
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
	l, err := p.protocol.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	return &kpListener{kp: p, Listener: l}, nil
}

func (p *killProtocol) Resolve(ctx *context.T, protocol, address string) (string, string, error) {
	return p.protocol.Resolve(ctx, "tcp", address)
}

func setEndpointRoutingID(ep naming.Endpoint, rid naming.RoutingID) (naming.Endpoint, error) {
	network, address, _, mountable := getEndpointParts(ep)
	var opts []naming.EndpointOpt
	opts = append(opts, rid)
	opts = append(opts, mountable)
	epString := naming.FormatEndpoint(network, address, opts...)
	return v23.NewEndpoint(epString)
}

// getEndpointParts returns all the fields of ep.
func getEndpointParts(ep naming.Endpoint) (network string, address string,
	rid naming.RoutingID, mountable naming.EndpointOpt) {
	network, address = ep.Addr().Network(), ep.Addr().String()
	rid = ep.RoutingID()
	mountable = naming.ServesMountTable(ep.ServesMountTable())
	return
}
