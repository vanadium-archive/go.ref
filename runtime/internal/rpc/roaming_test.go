// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"net"
	"reflect"
	"sort"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/x/lib/netstate"
	"v.io/x/lib/set"
	"v.io/x/ref/lib/pubsub"
	inaming "v.io/x/ref/runtime/internal/naming"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/ws"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/wsh"
	imanager "v.io/x/ref/runtime/internal/rpc/stream/manager"
	tnaming "v.io/x/ref/runtime/internal/testing/mocks/naming"
	"v.io/x/ref/test/testutil"
)

// TODO(mattr): Transition this to using public API.
func TestRoaming(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()

	publisher := pubsub.NewPublisher()
	roaming := make(chan pubsub.Setting)
	stop, err := publisher.CreateStream("TestRoaming", "TestRoaming", roaming)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { publisher.Shutdown(); <-stop }()

	nctx, _ := v23.WithPrincipal(ctx, testutil.NewPrincipal("test"))
	server, err := testInternalNewServerWithPubsub(nctx, sm, ns, publisher, "TestRoaming")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	ipv4And6 := netstate.AddressChooserFunc(func(network string, addrs []net.Addr) ([]net.Addr, error) {
		accessible := netstate.ConvertToAddresses(addrs)
		ipv4 := accessible.Filter(netstate.IsUnicastIPv4)
		ipv6 := accessible.Filter(netstate.IsUnicastIPv6)
		return append(ipv4.AsNetAddrs(), ipv6.AsNetAddrs()...), nil
	})
	spec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{
			{"tcp", "*:0"},
			{"tcp", ":0"},
			{"tcp", ":0"},
		},
		AddressChooser: ipv4And6,
	}

	eps, err := server.Listen(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) == 0 {
		t.Fatal("no endpoints listened on.")
	}

	if err = server.Serve("foo", &testServer{}, nil); err != nil {
		t.Fatal(err)
	}
	setLeafEndpoints(eps)
	if err = server.AddName("bar"); err != nil {
		t.Fatal(err)
	}

	status := server.Status()
	if got, want := status.Endpoints, eps; !cmpEndpoints(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if got, want := len(status.Mounts), len(eps)*2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}

	watcher := make(chan rpc.NetworkChange, 10)
	server.WatchNetwork(watcher)
	defer close(watcher)

	waitForChange := func() *rpc.NetworkChange {
		ctx.Infof("Waiting on %p", watcher)
		select {
		case c := <-watcher:
			return &c
		case <-time.After(time.Minute):
			t.Fatalf("timedout: %s", loc(1))
		}
		return nil
	}

	n1 := netstate.NewNetAddr("ip", "1.1.1.1")
	n2 := netstate.NewNetAddr("ip", "2.2.2.2")
	n3 := netstate.NewNetAddr("ip", "3.3.3.3")

	roaming <- NewNewAddrsSetting([]net.Addr{n1, n2})

	// We expect 2 added addrs and 4 changes, one for each IP per usable listen spec addr.
	change := waitForChange()
	if got, want := len(change.AddedAddrs), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got, want := len(change.Changed), 4; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}

	nepsA := make([]naming.Endpoint, len(eps))
	copy(nepsA, eps)
	for _, p := range getUniqPorts(eps) {
		nep1 := updateHost(eps[0], net.JoinHostPort("1.1.1.1", p))
		nep2 := updateHost(eps[0], net.JoinHostPort("2.2.2.2", p))
		nepsA = append(nepsA, []naming.Endpoint{nep1, nep2}...)
	}

	status = server.Status()
	if got, want := status.Endpoints, nepsA; !cmpEndpoints(got, want) {
		t.Fatalf("got %v, want %v [%d, %d]", got, want, len(got), len(want))
	}

	if got, want := len(status.Mounts), len(nepsA)*2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got, want := len(status.Mounts.Servers()), len(nepsA); got != want {
		t.Fatalf("got %d, want %d", got, want)
	}

	// Mimic that n2 has been changed to n3. The network monitor will publish
	// two AddrsSettings - RmAddrsSetting(n2) and NewNewAddrsSetting(n1, n3).

	roaming <- NewRmAddrsSetting([]net.Addr{n2})

	// We expect 1 removed addr and 2 changes, one for each usable listen spec addr.
	change = waitForChange()
	if got, want := len(change.RemovedAddrs), 1; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got, want := len(change.Changed), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}

	nepsR := make([]naming.Endpoint, len(eps))
	copy(nepsR, eps)
	for _, p := range getUniqPorts(eps) {
		nep2 := updateHost(eps[0], net.JoinHostPort("1.1.1.1", p))
		nepsR = append(nepsR, nep2)
	}

	status = server.Status()
	if got, want := status.Endpoints, nepsR; !cmpEndpoints(got, want) {
		t.Fatalf("got %v, want %v [%d, %d]", got, want, len(got), len(want))
	}

	roaming <- NewNewAddrsSetting([]net.Addr{n1, n3})

	// We expect 1 added addr and 2 changes, one for the new IP per usable listen spec addr.
	change = waitForChange()
	if got, want := len(change.AddedAddrs), 1; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got, want := len(change.Changed), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}

	nepsC := make([]naming.Endpoint, len(eps))
	copy(nepsC, eps)
	for _, p := range getUniqPorts(eps) {
		nep1 := updateHost(eps[0], net.JoinHostPort("1.1.1.1", p))
		nep2 := updateHost(eps[0], net.JoinHostPort("3.3.3.3", p))
		nepsC = append(nepsC, nep1, nep2)
	}

	status = server.Status()
	if got, want := status.Endpoints, nepsC; !cmpEndpoints(got, want) {
		t.Fatalf("got %v, want %v [%d, %d]", got, want, len(got), len(want))
	}

	if got, want := len(status.Mounts), len(nepsC)*2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got, want := len(status.Mounts.Servers()), len(nepsC); got != want {
		t.Fatalf("got %d, want %d", got, want)
	}

	// Remove all addresses to mimic losing all connectivity.
	roaming <- NewRmAddrsSetting(getIPAddrs(nepsC))

	// We expect changes for all of the current endpoints
	change = waitForChange()
	if got, want := len(change.Changed), len(nepsC); got != want {
		t.Fatalf("got %d, want %d", got, want)
	}

	status = server.Status()
	if got, want := len(status.Mounts), 0; got != want {
		t.Fatalf("got %d, want %d: %v", got, want, status.Mounts)
	}

	roaming <- NewNewAddrsSetting([]net.Addr{n1})
	// We expect 2 changes, one for each usable listen spec addr.
	change = waitForChange()
	if got, want := len(change.Changed), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestWatcherDeadlock(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()

	publisher := pubsub.NewPublisher()
	roaming := make(chan pubsub.Setting)
	stop, err := publisher.CreateStream("TestWatcherDeadlock", "TestWatcherDeadlock", roaming)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { publisher.Shutdown(); <-stop }()

	nctx, _ := v23.WithPrincipal(ctx, testutil.NewPrincipal("test"))
	server, err := testInternalNewServerWithPubsub(nctx, sm, ns, publisher, "TestWatcherDeadlock")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	spec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{
			{"tcp", ":0"},
		},
	}
	eps, err := server.Listen(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.Serve("foo", &testServer{}, nil); err != nil {
		t.Fatal(err)
	}
	setLeafEndpoints(eps)

	// Set a watcher that we never read from - the intent is to make sure
	// that the listener still listens to changes even though there is no
	// goroutine to read from the watcher channel.
	watcher := make(chan rpc.NetworkChange, 0)
	server.WatchNetwork(watcher)
	defer close(watcher)

	// Remove all addresses to mimic losing all connectivity.
	roaming <- NewRmAddrsSetting(getIPAddrs(eps))

	// Add in two new addresses
	n1 := netstate.NewNetAddr("ip", "1.1.1.1")
	n2 := netstate.NewNetAddr("ip", "2.2.2.2")
	roaming <- NewNewAddrsSetting([]net.Addr{n1, n2})

	neps := make([]naming.Endpoint, 0, len(eps))
	for _, p := range getUniqPorts(eps) {
		nep1 := updateHost(eps[0], net.JoinHostPort("1.1.1.1", p))
		nep2 := updateHost(eps[0], net.JoinHostPort("2.2.2.2", p))
		neps = append(neps, []naming.Endpoint{nep1, nep2}...)
	}
	then := time.Now()
	for {
		status := server.Status()
		if got, want := status.Endpoints, neps; cmpEndpoints(got, want) {
			break
		}
		time.Sleep(100 * time.Millisecond)
		if time.Now().Sub(then) > time.Minute {
			t.Fatalf("timed out waiting for changes to take effect")
		}
	}
}

func updateHost(ep naming.Endpoint, address string) naming.Endpoint {
	niep := *(ep).(*inaming.Endpoint)
	niep.Address = address
	return &niep
}

func getIPAddrs(eps []naming.Endpoint) []net.Addr {
	hosts := map[string]struct{}{}
	for _, ep := range eps {
		iep := (ep).(*inaming.Endpoint)
		h, _, _ := net.SplitHostPort(iep.Address)
		if len(h) > 0 {
			hosts[h] = struct{}{}
		}
	}
	addrs := []net.Addr{}
	for h, _ := range hosts {
		addrs = append(addrs, netstate.NewNetAddr("ip", h))
	}
	return addrs
}

func cmpEndpoints(got, want []naming.Endpoint) bool {
	if len(got) != len(want) {
		return false
	}
	return reflect.DeepEqual(endpointToStrings(got), endpointToStrings(want))
}

func getUniqPorts(eps []naming.Endpoint) []string {
	ports := map[string]struct{}{}
	for _, ep := range eps {
		iep := ep.(*inaming.Endpoint)
		_, p, _ := net.SplitHostPort(iep.Address)
		ports[p] = struct{}{}
	}
	return set.String.ToSlice(ports)
}

func endpointToStrings(eps []naming.Endpoint) []string {
	r := []string{}
	for _, ep := range eps {
		r = append(r, ep.String())
	}
	sort.Strings(r)
	return r
}

func setLeafEndpoints(eps []naming.Endpoint) {
	for i := range eps {
		eps[i].(*inaming.Endpoint).IsLeaf = true
	}
}
