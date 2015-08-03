// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager_test

import (
	"bufio"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/security"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/runtime/internal/flow/manager"
	"v.io/x/ref/test/testutil"
)

func TestDirectConnection(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	p := testutil.NewPrincipal("test")
	ctx, err := v23.WithPrincipal(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	rid := naming.FixedRoutingID(0x5555)
	m := manager.New(ctx, rid)
	want := "read this please"

	if err := m.Listen(ctx, "tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}

	bFn := func(*context.T, security.Call) (security.Blessings, error) { return p.BlessingStore().Default(), nil }
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

func readLine(f flow.Flow) (string, error) {
	s, err := bufio.NewReader(f).ReadString('\n')
	return strings.TrimRight(s, "\n"), err
}

func writeLine(f flow.Flow, data string) error {
	data += "\n"
	_, err := f.Write([]byte(data))
	return err
}
