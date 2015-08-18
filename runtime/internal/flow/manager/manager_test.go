// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"bufio"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/security"

	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/test"
)

func init() {
	test.Init()
}

func TestDirectConnection(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	p := v23.GetPrincipal(ctx)
	rid := naming.FixedRoutingID(0x5555)
	m := New(ctx, rid)
	want := "read this please"

	if err := m.Listen(ctx, "tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}

	bFn := func(
		ctx *context.T,
		localEndpoint, remoteEndpoint naming.Endpoint,
		remoteBlessings security.Blessings,
		remoteDischarges map[string]security.Discharge,
	) (security.Blessings, error) {
		return p.BlessingStore().Default(), nil
	}
	eps := m.ListeningEndpoints()
	if len(eps) == 0 {
		t.Fatalf("no endpoints listened on")
	}
	flow, err := m.Dial(ctx, eps[0], bFn)
	if err != nil {
		t.Error(err)
	}
	writeLine(flow, want)

	flow, err = m.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readLine(flow)
	if err != nil {
		t.Error(err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDialCachedConn(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	p := v23.GetPrincipal(ctx)
	am := New(ctx, naming.FixedRoutingID(0x5555))
	if err := am.Listen(ctx, "tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}

	bFn := func(
		ctx *context.T,
		localEndpoint, remoteEndpoint naming.Endpoint,
		remoteBlessings security.Blessings,
		remoteDischarges map[string]security.Discharge,
	) (security.Blessings, error) {
		return p.BlessingStore().Default(), nil
	}
	eps := am.ListeningEndpoints()
	if len(eps) == 0 {
		t.Fatalf("no endpoints listened on")
	}
	dm := New(ctx, naming.FixedRoutingID(0x1111))
	// At first the cache should be empty.
	if got, want := dm.(*manager).cache.Size(), 0; got != want {
		t.Fatalf("got cache size %v, want %v", got, want)
	}
	// After dialing a connection the cache should hold one connection.
	dialAndAccept(t, ctx, dm, am, eps[0], bFn)
	if got, want := dm.(*manager).cache.Size(), 1; got != want {
		t.Fatalf("got cache size %v, want %v", got, want)
	}
	// After dialing another connection the cache should still hold one connection
	// because the connections should be reused.
	dialAndAccept(t, ctx, dm, am, eps[0], bFn)
	if got, want := dm.(*manager).cache.Size(), 1; got != want {
		t.Fatalf("got cache size %v, want %v", got, want)
	}
}

func dialAndAccept(t *testing.T, ctx *context.T, dm, am flow.Manager, ep naming.Endpoint, bFn flow.BlessingsForPeer) (df, af flow.Flow) {
	var err error
	df, err = dm.Dial(ctx, ep, bFn)
	if err != nil {
		t.Fatal(err)
	}
	// Write a line to ensure that the openFlow message is sent.
	writeLine(df, "")
	af, err = am.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func readLine(f flow.Flow) (string, error) {
	s, err := bufio.NewReader(f).ReadString('\n')
	return strings.TrimRight(s, "\n"), err
}

func writeLine(f flow.Flow, data string) error {
	data += "\n"
	_, err := f.Write([]byte(data))
	return err
}
