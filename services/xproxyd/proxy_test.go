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

	// Start the proxy.
	addr := struct {
		Protocol, Address string
	}{
		Protocol: "tcp",
		Address:  "127.0.0.1:0",
	}
	pctx = v23.WithListenSpec(pctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{addr}})
	p, err := xproxyd.New(pctx)
	if err != nil {
		t.Fatal(err)
	}
	peps := p.ListeningEndpoints()
	if len(peps) == 0 {
		t.Fatal("Proxy not listening on any endpoints")
	}
	pep := peps[0]

	t.Logf("proxy endpoint: %s", pep.String())
	// Start a accepting flow.Manager and make it listen through the proxy.
	if err := am.Listen(actx, "v23", pep.String()); err != nil {
		t.Fatal(err)
	}
	aeps := am.ListeningEndpoints()
	if len(aeps) == 0 {
		t.Fatal("Acceptor not listening on any endpoints")
	}
	aep := aeps[0]

	// The dialing flow.Manager dials a flow to the accepting flow.Manager.
	want := "Do you read me?"
	bFn := func(
		ctx *context.T,
		localEndpoint, remoteEndpoint naming.Endpoint,
		remoteBlessings security.Blessings,
		remoteDischarges map[string]security.Discharge,
	) (security.Blessings, map[string]security.Discharge, error) {
		return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
	}
	df, err := dm.Dial(dctx, aep, bFn)
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
		pctx.Errorf("error")
		t.Fatal(err)
	}
	if got != want {
		pctx.Errorf("error")
		t.Errorf("got %v, want %v", got, want)
	}

	// Writes in the opposite direction should work as well.
	want = "I read you loud and clear."
	writeLine(af, want)
	got, err = readLine(df)
	if err != nil {
		pctx.Errorf("error")
		t.Fatal(err)
	}
	if got != want {
		pctx.Errorf("error")
		t.Errorf("got %v, want %v", got, want)
	}
}

// TODO(suharshs): Add tests for multiple proxies and bidirectional RPC through
// a proxy once we have bidirpc working.

func readLine(f flow.Flow) (string, error) {
	s, err := bufio.NewReader(f).ReadString('\n')
	return strings.TrimRight(s, "\n"), err
}

func writeLine(f flow.Flow, data string) error {
	data += "\n"
	_, err := f.Write([]byte(data))
	return err
}
