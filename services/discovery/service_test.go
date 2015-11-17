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
			InstanceId:    "123",
			InterfaceName: "v.io/v23/a",
			Attrs:         discovery.Attributes{"a1": "v1"},
			Addrs:         []string{"/h1:123/x"},
		},
		{
			InterfaceName: "v.io/v23/b",
			Attrs:         discovery.Attributes{"b1": "v1"},
			Addrs:         []string{"/h1:123/y"},
		},
	}

	var handles []sdiscovery.ServiceHandle
	advertiser := sdiscovery.AdvertiserClient(addr)
	for i, service := range services {
		handle, instanceId, err := advertiser.RegisterService(ctx, service, nil)
		if err != nil {
			t.Fatalf("RegisterService() failed: %v", err)
		}
		switch {
		case len(service.InstanceId) == 0:
			if len(instanceId) == 0 {
				t.Errorf("test[%d]: got empty instance id", i)
			}
			services[i].InstanceId = instanceId
		default:
			if instanceId != service.InstanceId {
				t.Errorf("test[%d]: got instance id %v, but wanted %v", i, instanceId, service.InstanceId)
			}
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
