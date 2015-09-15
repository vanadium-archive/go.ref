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

	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/runtime/internal/flow/conn"
	"v.io/x/ref/runtime/internal/flow/flowtest"
	"v.io/x/ref/test"
)

func init() {
	test.Init()
}

func TestDirectConnection(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	rid := naming.FixedRoutingID(0x5555)
	m := New(ctx, rid)

	if err := m.Listen(ctx, "tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}

	testFlows(t, ctx, m, m, flowtest.BlessingsForPeer)
}

func TestDialCachedConn(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	am := New(ctx, naming.FixedRoutingID(0x5555))
	if err := am.Listen(ctx, "tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}

	dm := New(ctx, naming.FixedRoutingID(0x1111))
	// At first the cache should be empty.
	if got, want := len(dm.(*manager).cache.addrCache), 0; got != want {
		t.Fatalf("got cache size %v, want %v", got, want)
	}
	// After dialing a connection the cache should hold one connection.
	testFlows(t, ctx, dm, am, flowtest.BlessingsForPeer)
	if got, want := len(dm.(*manager).cache.addrCache), 1; got != want {
		t.Fatalf("got cache size %v, want %v", got, want)
	}
	// After dialing another connection the cache should still hold one connection
	// because the connections should be reused.
	testFlows(t, ctx, dm, am, flowtest.BlessingsForPeer)
	if got, want := len(dm.(*manager).cache.addrCache), 1; got != want {
		t.Fatalf("got cache size %v, want %v", got, want)
	}
}

func TestBidirectionalListeningEndpoint(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	am := New(ctx, naming.FixedRoutingID(0x5555))
	if err := am.Listen(ctx, "tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	eps := am.ListeningEndpoints()
	if len(eps) == 0 {
		t.Fatalf("no endpoints listened on")
	}
	dm := New(ctx, naming.FixedRoutingID(0x1111))
	testFlows(t, ctx, dm, am, flowtest.BlessingsForPeer)
	// Now am should be able to make a flow to dm even though dm is not listening.
	testFlows(t, ctx, am, dm, flowtest.BlessingsForPeer)
}

func TestNullClientBlessings(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	am := New(ctx, naming.FixedRoutingID(0x5555))
	if err := am.Listen(ctx, "tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	dm := New(ctx, naming.NullRoutingID)
	_, af := testFlows(t, ctx, dm, am, flowtest.BlessingsForPeer)
	// Ensure that the remote blessings of the underlying conn of the accepted flow are zero.
	if rBlessings := af.Conn().(*conn.Conn).RemoteBlessings(); !rBlessings.IsZero() {
		t.Errorf("got %v, want zero-value blessings", rBlessings)
	}
	dm = New(ctx, naming.FixedRoutingID(0x1111))
	_, af = testFlows(t, ctx, dm, am, flowtest.BlessingsForPeer)
	// Ensure that the remote blessings of the underlying conn of the accepted flow are
	// non-zero if we did specify a RoutingID.
	if rBlessings := af.Conn().(*conn.Conn).RemoteBlessings(); rBlessings.IsZero() {
		t.Errorf("got %v, want non-zero blessings", rBlessings)
	}
}

func testFlows(t *testing.T, ctx *context.T, dm, am flow.Manager, bFn flow.BlessingsForPeer) (df, af flow.Flow) {
	eps := am.ListeningEndpoints()
	if len(eps) == 0 {
		t.Fatalf("no endpoints listened on")
	}
	ep := eps[0]
	var err error
	df, err = dm.Dial(ctx, ep, bFn)
	if err != nil {
		t.Fatal(err)
	}
	want := "do you read me?"
	writeLine(df, want)
	af, err = am.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}

	got, err := readLine(af)
	if err != nil {
		t.Error(err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	want = "i read you"
	if err := writeLine(af, want); err != nil {
		t.Error(err)
	}
	got, err = readLine(df)
	if err != nil {
		t.Error(err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
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
