// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/stats"
	"v.io/x/lib/vlog"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

// TODO(toddw): Add tests of Signature and MethodSignature.

func TestProxyInvoker(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	// server1 is a normal server
	server1, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	localSpec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	eps1, err := server1.Listen(localSpec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := server1.Serve("", &dummy{}, nil); err != nil {
		t.Fatalf("server1.Serve: %v", err)
	}

	// server2 proxies requests to <suffix> to server1/__debug/stats/<suffix>
	server2, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server2.Stop()
	eps2, err := server2.Listen(localSpec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	disp := &proxyDispatcher{
		naming.JoinAddressName(eps1[0].String(), "__debug/stats"),
		stats.StatsServer(nil).Describe__(),
	}
	if err := server2.ServeDispatcher("", disp); err != nil {
		t.Fatalf("server2.Serve: %v", err)
	}

	// Call Value()
	name := naming.JoinAddressName(eps2[0].String(), "system/start-time-rfc1123")
	c := stats.StatsClient(name)
	if _, err := c.Value(ctx); err != nil {
		t.Fatalf("%q.Value() error: %v", name, err)
	}

	// Call Glob()
	results, _, err := testutil.GlobName(ctx, naming.JoinAddressName(eps2[0].String(), "system"), "start-time-*")
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}
	expected := []string{
		"start-time-rfc1123",
		"start-time-unix",
	}
	if !reflect.DeepEqual(results, expected) {
		t.Errorf("unexpected results. Got %q, want %q", results, expected)
	}
}

type dummy struct{}

func (*dummy) Method(*context.T, rpc.ServerCall) error { return nil }

type proxyDispatcher struct {
	remote string
	desc   []rpc.InterfaceDesc
}

func (d *proxyDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	vlog.Infof("LOOKUP(%s): remote .... %s", suffix, d.remote)
	return newProxyInvoker(naming.Join(d.remote, suffix), access.Debug, d.desc), nil, nil
}
