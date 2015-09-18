// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxyd_test

import (
	"bufio"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/xproxyd"
	"v.io/x/ref/test/goroutines"

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
)

func TestSingleProxy(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	kp := newKillProtocol()
	flow.RegisterProtocol("kill", kp)
	pctx, shutdown := v23.Init()
	defer shutdown()
	actx, am, err := v23.ExperimentalWithNewFlowManager(pctx)
	if err != nil {
		t.Fatal(err)
	}
	dctx, dm, err := v23.ExperimentalWithNewFlowManager(pctx)
	if err != nil {
		t.Fatal(err)
	}

	pep := startProxy(t, pctx, address{"kill", "127.0.0.1:0"})

	if err := am.Listen(actx, "v23", pep.String()); err != nil {
		t.Fatal(err)
	}

	for am.ListeningEndpoints()[0].Addr().Network() == "" {
		time.Sleep(pollTime)
	}

	testEndToEndConnections(t, dctx, actx, dm, am, kp)
}

func TestMultipleProxies(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	kp := newKillProtocol()
	flow.RegisterProtocol("kill", kp)
	pctx, shutdown := v23.Init()
	defer shutdown()
	actx, am, err := v23.ExperimentalWithNewFlowManager(pctx)
	if err != nil {
		t.Fatal(err)
	}
	dctx, dm, err := v23.ExperimentalWithNewFlowManager(pctx)
	if err != nil {
		t.Fatal(err)
	}

	pep := startProxy(t, pctx, address{"kill", "127.0.0.1:0"})

	p2ep := startProxy(t, pctx, address{"v23", pep.String()}, address{"kill", "127.0.0.1:0"})

	p3ep := startProxy(t, pctx, address{"v23", p2ep.String()}, address{"kill", "127.0.0.1:0"})

	if err := am.Listen(actx, "v23", p3ep.String()); err != nil {
		t.Fatal(err)
	}

	// Wait for am.Listen to get 3 endpoints.
	for len(am.ListeningEndpoints()) != 3 {
		time.Sleep(pollTime)
	}

	testEndToEndConnections(t, dctx, actx, dm, am, kp)
}

func testEndToEndConnections(t *testing.T, dctx, actx *context.T, dm, am flow.Manager, kp *killProtocol) {
	aeps := am.ListeningEndpoints()
	if len(aeps) == 0 {
		t.Fatal("acceptor not listening on any endpoints")
	}
	for _, aep := range aeps {
		// Kill the connections, connections should still eventually succeed.
		kp.KillConnections()
		for {
			if err := testEndToEndConnection(t, dctx, actx, dm, am, aep); err != nil {
				t.Log(err)
				time.Sleep(pollTime)
				continue
			}
			break
		}
	}
}

func testEndToEndConnection(t *testing.T, dctx, actx *context.T, dm, am flow.Manager, aep naming.Endpoint) error {
	// The dialing flow.Manager dials a flow to the accepting flow.Manager.
	want := "Do you read me?"
	df, err := dm.Dial(dctx, aep, bfp)
	if err != nil {
		return err
	}
	// We write before accepting to ensure that the openFlow message is sent.
	if err := writeLine(df, want); err != nil {
		return err
	}
	af, err := am.Accept(actx)
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

func bfp(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) (security.Blessings, map[string]security.Discharge, error) {
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
