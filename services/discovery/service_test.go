// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	sdiscovery "v.io/v23/services/discovery"

	idiscovery "v.io/x/ref/lib/discovery"
	fdiscovery "v.io/x/ref/lib/discovery/factory"
	"v.io/x/ref/lib/discovery/plugins/mock"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

func TestBasic(t *testing.T) {
	fdiscovery.InjectDiscovery(idiscovery.NewWithPlugins([]idiscovery.Plugin{mock.New()}))
	ctx, shutdown := test.V23Init()
	defer shutdown()

	ds := NewDiscoveryService(ctx)
	ctx, server, err := v23.WithNewServer(ctx, "", sdiscovery.DiscoveryServer(ds), nil)
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()
	addr := server.Status().Endpoints[0].Name()

	services := []discovery.Service{
		{
			InstanceUuid:  idiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/v23/a",
			Attrs:         discovery.Attributes{"a1": "v1"},
			Addrs:         []string{"/h1:123/x"},
		},
		{
			InstanceUuid:  idiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/v23/b",
			Attrs:         discovery.Attributes{"b1": "v1"},
			Addrs:         []string{"/h1:123/y"},
		},
	}

	var handles []sdiscovery.ServiceHandle
	advertiser := sdiscovery.AdvertiserClient(addr)
	for _, service := range services {
		handle, err := advertiser.RegisterService(ctx, service, nil)
		if err != nil {
			t.Fatalf("RegisterService() failed: %v", err)
		}
		handles = append(handles, handle)
	}

	scanner := sdiscovery.ScannerClient(addr)
	if err := scanAndMatch(ctx, scanner, "", services...); err != nil {
		t.Error(err)
	}

	if err := advertiser.UnregisterService(ctx, handles[0]); err != nil {
		t.Fatalf("UnregisterService() failed: %v", err)
	}
	if err := scanAndMatch(ctx, scanner, "", services[1]); err != nil {
		t.Error(err)
	}
}

func scanAndMatch(ctx *context.T, scanner sdiscovery.ScannerClientStub, query string, wants ...discovery.Service) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := scanner.Scan(ctx, query)
	if err != nil {
		return err
	}

	recv := stream.RecvStream()
	for len(wants) > 0 {
		if !recv.Advance() {
			return recv.Err()
		}
		found := recv.Value().Interface().(discovery.Found)
		matched := false
		for i, want := range wants {
			if reflect.DeepEqual(found.Service, want) {
				wants = append(wants[:i], wants[i+1:]...)
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("unexpected service found: %v", found.Service)
		}
	}

	// Make sure there is no more update.
	time.AfterFunc(5*time.Millisecond, cancel)
	if recv.Advance() {
		return fmt.Errorf("unexpected update: %v", recv.Value())
	}
	return nil
}
