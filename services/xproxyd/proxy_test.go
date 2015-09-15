// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxyd_test

import (
	"bufio"
	"strings"
	"testing"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/xproxyd"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

func TestProxiedConnection(t *testing.T) {
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

	pep := startProxy(t, pctx, address{"tcp", "127.0.0.1:0"})

	if err := am.Listen(actx, "v23", pep.String()); err != nil {
		t.Fatal(err)
	}
	testEndToEndConnections(t, dctx, actx, dm, am)
}

func TestMultipleProxies(t *testing.T) {
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

	pep := startProxy(t, pctx, address{"tcp", "127.0.0.1:0"})

	p2ep := startProxy(t, pctx, address{"v23", pep.String()}, address{"tcp", "127.0.0.1:0"})

	p3ep := startProxy(t, pctx, address{"v23", p2ep.String()}, address{"tcp", "127.0.0.1:0"})

	if err := am.Listen(actx, "v23", p3ep.String()); err != nil {
		t.Fatal(err)
	}
	testEndToEndConnections(t, dctx, actx, dm, am)
}

func testEndToEndConnections(t *testing.T, dctx, actx *context.T, dm, am flow.Manager) {
	aeps := am.ListeningEndpoints()
	if len(aeps) == 0 {
		t.Fatal("acceptor not listening on any endpoints")
	}
	for _, aep := range aeps {
		testEndToEndConnection(t, dctx, actx, dm, am, aep)
	}
}

func testEndToEndConnection(t *testing.T, dctx, actx *context.T, dm, am flow.Manager, aep naming.Endpoint) {
	// The dialing flow.Manager dials a flow to the accepting flow.Manager.
	want := "Do you read me?"
	df, err := dm.Dial(dctx, aep, bfp)
	if err != nil {
		t.Fatal(err)
	}
	// We write before accepting to ensure that the openFlow message is sent.
	writeLine(df, want)
	af, err := am.Accept(actx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readLine(af)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	// Writes in the opposite direction should work as well.
	want = "I read you loud and clear."
	writeLine(af, want)
	got, err = readLine(df)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
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
	for _, addr := range addrs {
		ls.Addrs = append(ls.Addrs, addr)
	}
	ctx = v23.WithListenSpec(ctx, ls)
	proxy, _, err := xproxyd.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	peps := proxy.ListeningEndpoints()
	for _, pep := range peps {
		if pep.Addr().Network() == "tcp" {
			return pep
		}
	}
	t.Fatal("Proxy not listening on network address.")
	return nil
}
