// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"
	"v.io/v23/rpc"

	idiscovery "v.io/x/ref/lib/discovery"
	fdiscovery "v.io/x/ref/lib/discovery/factory"
	"v.io/x/ref/lib/discovery/plugins/mock"
	"v.io/x/ref/lib/discovery/util"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

type mockServer struct {
	eps             []naming.Endpoint
	watcher         chan<- rpc.NetworkChange
	watcherClosedCh chan struct{}
}

func (s *mockServer) AddName(string) error     { return nil }
func (s *mockServer) RemoveName(string)        {}
func (s *mockServer) Stop() error              { return nil }
func (s *mockServer) Closed() <-chan struct{}  { return nil }
func (s *mockServer) Status() rpc.ServerStatus { return rpc.ServerStatus{Endpoints: s.eps} }

func (s *mockServer) WatchNetwork(ch chan<- rpc.NetworkChange) {
	s.watcher = ch
	s.watcherClosedCh = make(chan struct{})
}

func (s *mockServer) UnwatchNetwork(ch chan<- rpc.NetworkChange) {
	s.watcher = nil
	close(s.watcherClosedCh)
}

func (s *mockServer) updateNetwork(eps []naming.Endpoint) {
	s.eps = eps
	if s.watcher != nil {
		s.watcher <- rpc.NetworkChange{Changed: eps}
	}
}

func (s *mockServer) watcherClosed() <-chan struct{} {
	return s.watcherClosedCh
}

func newMockServer(eps []naming.Endpoint) *mockServer {
	return &mockServer{eps: eps}
}

func newEndpoints(addrs ...string) []naming.Endpoint {
	eps := make([]naming.Endpoint, len(addrs))
	for i, a := range addrs {
		eps[i], _ = v23.NewEndpoint(a)
	}
	return eps
}

func TestNetworkChange(t *testing.T) {
	fdiscovery.InjectDiscovery(idiscovery.NewWithPlugins([]idiscovery.Plugin{mock.New()}))
	ctx, shutdown := test.V23Init()
	defer shutdown()

	service := discovery.Service{
		InstanceUuid:  idiscovery.NewInstanceUUID(),
		InterfaceName: "v.io/v23/a",
		Attrs:         discovery.Attributes{"a1": "v1"},
	}

	const suffix = "test"
	eps := newEndpoints("addr1:123")
	mock := newMockServer(eps)

	ctx, cancel := context.WithCancel(ctx)
	util.AdvertiseServer(ctx, mock, suffix, service, nil)
	if err := scanAndMatch(ctx, service, eps, suffix); err != nil {
		t.Error(err)
	}

	tests := [][]naming.Endpoint{
		newEndpoints("addr2:123", "addr3:456"),
		newEndpoints("addr4:123"),
		newEndpoints("addr5:123", "addr6:456"),
	}
	for _, eps := range tests {
		mock.updateNetwork(eps)
		if err := scanAndMatch(ctx, service, eps, suffix); err != nil {
			t.Error(err)
		}
	}

	// Make sure that the network watcher is unregistered when the context
	// is canceled.
	cancel()

	select {
	case <-mock.watcherClosed():
	case <-time.After(3 * time.Second):
		t.Error("watcher not closed")
	}
}

func TestNetworkChangeInstanceUuid(t *testing.T) {
	fdiscovery.InjectDiscovery(idiscovery.NewWithPlugins([]idiscovery.Plugin{mock.New()}))
	ctx, shutdown := test.V23Init()
	defer shutdown()

	mock := newMockServer(newEndpoints("addr1:123"))
	util.AdvertiseServer(ctx, mock, "", discovery.Service{InterfaceName: "v.io/v23/a"}, nil)

	// Scan the advertised service.
	service, err := scan(ctx, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(service.InstanceUuid) == 0 {
		t.Fatal("couldn't scan")
	}

	// Make sure the instance uuid has not been changed.
	eps := newEndpoints("addr2:123")
	mock.updateNetwork(eps)
	if err := scanAndMatch(ctx, service, eps, ""); err != nil {
		t.Error(err)
	}
}

func scanAndMatch(ctx *context.T, want discovery.Service, eps []naming.Endpoint, suffix string) error {
	want.Addrs = make([]string, len(eps))
	for i, ep := range eps {
		want.Addrs[i] = naming.JoinAddressName(ep.Name(), suffix)
	}

	const timeout = 3 * time.Second

	var found discovery.Service
	for now := time.Now(); time.Since(now) < timeout; {
		var err error
		found, err = scan(ctx, 5*time.Millisecond)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(found, want) {
			return nil
		}
	}
	return fmt.Errorf("match failed; got %v, but wanted %v", found, want)
}

func scan(ctx *context.T, timeout time.Duration) (discovery.Service, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ds := v23.GetDiscovery(ctx)
	scan, err := ds.Scan(ctx, "")
	if err != nil {
		return discovery.Service{}, fmt.Errorf("scan failed: %v", err)
	}

	select {
	case update := <-scan:
		return update.Interface().(discovery.Found).Service, nil
	case <-time.After(timeout):
		return discovery.Service{}, nil
	}
}
