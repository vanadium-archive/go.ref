// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"net"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/pubsub"
	"v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/runtime/internal/lib/roaming"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

type testServer struct{}

func (t *testServer) Foo(*context.T, rpc.ServerCall) error { return nil }

func TestRoaming(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	waitForEndpoints := func(server rpc.Server, n int) rpc.ServerStatus {
		for {
			status := server.Status()
			if got, want := len(status.Endpoints), n; got != want {
				<-status.Dirty
			} else {
				return status
			}
		}
	}

	ctx = fake.SetClientFactory(ctx, func(ctx *context.T, opts ...rpc.ClientOpt) rpc.Client {
		return NewClient(ctx, opts...)
	})

	publisher := pubsub.NewPublisher()
	ch := make(chan pubsub.Setting)
	stop, err := publisher.CreateStream(roaming.RoamingSetting, roaming.RoamingSettingDesc, ch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { publisher.Shutdown(); <-stop }()

	ipv4And6 := netstate.AddressChooserFunc(func(network string, addrs []net.Addr) ([]net.Addr, error) {
		accessible := netstate.ConvertToAddresses(addrs)
		ipv4 := accessible.Filter(netstate.IsUnicastIPv4)
		ipv6 := accessible.Filter(netstate.IsUnicastIPv6)
		return append(ipv4.AsNetAddrs(), ipv6.AsNetAddrs()...), nil
	})
	spec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{
			{"tcp", "*:0"}, // an invalid address.
			{"tcp", ":0"},
			{"tcp", ":0"},
		},
		AddressChooser: ipv4And6,
	}
	sctx := v23.WithListenSpec(ctx, spec)
	sctx, _ = v23.WithPrincipal(sctx, testutil.NewPrincipal("test"))
	sctx, server, err := WithNewServer(sctx, "foo", &testServer{}, nil, publisher)
	if err != nil {
		t.Error(err)
	}
	status := server.Status()
	prevEps := status.Endpoints

	n1 := netstate.NewNetAddr("ip", "1.1.1.1")
	n2 := netstate.NewNetAddr("ip", "2.2.2.2")

	ch <- roaming.NewUpdateAddrsSetting([]net.Addr{n1, n2})
	// We expect 4 new endpoints, 2 for each valid listen call.
	status = waitForEndpoints(server, len(prevEps)+4)
	eps := status.Endpoints
	// We expect the added networks to be in the new endpoints.
	if got, want := len(filterEndpointsByHost(eps, "1.1.1.1")), 2; got != want {
		t.Errorf("got %v, wanted %v endpoints with host 1.1.1.1", got, want)
	}
	if got, want := len(filterEndpointsByHost(eps, "2.2.2.2")), 2; got != want {
		t.Errorf("got %v, wanted %v endpoints with host 2.2.2.2", got, want)
	}
	prevEps = eps

	// Now remove a network.
	ch <- roaming.NewRmAddrsSetting([]net.Addr{n1})
	status = waitForEndpoints(server, len(prevEps)-2)
	eps = status.Endpoints
	// We expect the removed network to not be in the new endpoints.
	if got, want := len(filterEndpointsByHost(eps, "1.1.1.1")), 0; got != want {
		t.Errorf("got %v, wanted %v endpoints with host 1.1.1.1", got, want)
	}
	prevEps = eps

	// Now remove everything, essentially "disconnected from the network"
	ch <- roaming.NewRmAddrsSetting(getIPAddrs(prevEps))
	// We expect there to be only the bidi endpoint.
	status = waitForEndpoints(server, 1)
	eps = status.Endpoints
	if got, want := len(eps), 1; got != want && eps[0].Addr().Network() != "bidi" {
		t.Errorf("got %v, want %v", got, want)
	}

	// Now if we reconnect to a network it should should up.
	ch <- roaming.NewUpdateAddrsSetting([]net.Addr{n1})
	// We expect 2 endpoints to be added
	status = waitForEndpoints(server, 2)
	eps = status.Endpoints
	if got, want := len(eps), 2; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	// We expect the removed network to not be in the new endpoints.
	if got, want := len(filterEndpointsByHost(eps, "1.1.1.1")), 2; got != want {
		t.Errorf("got %v, wanted %v endpoints with host 1.1.1.1", got, want)
	}
}

func filterEndpointsByHost(eps []naming.Endpoint, host string) []naming.Endpoint {
	var filtered []naming.Endpoint
	for _, ep := range eps {
		if strings.Contains(ep.Addr().String(), host) {
			filtered = append(filtered, ep)
		}
	}
	return filtered
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
