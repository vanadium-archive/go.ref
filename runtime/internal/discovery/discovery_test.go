// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"reflect"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/runtime/internal/discovery"
	"v.io/x/ref/runtime/internal/discovery/plugins/mock"
)

func TestBasic(t *testing.T) {
	ds := idiscovery.New([]idiscovery.Plugin{mock.New()})
	services := []discovery.Service{
		{
			InstanceUuid:  idiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/v23/a",
			Addrs:         []string{"/h1:123/x", "/h2:123/y"},
		},
		{
			InstanceUuid:  idiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/v23/b",
			Addrs:         []string{"/h1:123/x", "/h2:123/z"},
		},
	}
	var stops []func()
	for _, service := range services {
		stop, err := advertise(ds, service)
		if err != nil {
			t.Fatalf("Advertise failed: %v\n", err)
		}
		stops = append(stops, stop)
	}

	updates, err := scan(ds, "v.io/v23/a")
	if err != nil {
		t.Fatalf("Scan failed: %v\n", err)
	}
	if !match(updates, services[0]) {
		t.Errorf("Scan failed; got %v, but wanted %v\n", updates, services[0])
	}
	updates, err = scan(ds, "v.io/v23/b")
	if err != nil {
		t.Fatalf("Scan failed: %v\n", err)
	}
	if !match(updates, services[1]) {
		t.Errorf("Scan failed; got %v, but wanted %v\n", updates, services[1])
	}
	updates, err = scan(ds, "")
	if err != nil {
		t.Fatalf("Scan failed: %v\n", err)
	}
	if !match(updates, services...) {
		t.Errorf("Scan failed; got %v, but wanted %v\n", updates, services)
	}
	updates, err = scan(ds, "v.io/v23/c")
	if err != nil {
		t.Fatalf("Scan failed: %v\n", err)
	}
	if !match(updates) {
		t.Errorf("Scan failed; got %v, but wanted %v\n", updates, nil)
	}

	// Stop advertising the first service. Shouldn't affect the other.
	stops[0]()
	updates, err = scan(ds, "v.io/v23/a")
	if err != nil {
		t.Fatalf("Scan failed: %v\n", err)
	}
	if !match(updates) {
		t.Errorf("Scan failed; got %v, but wanted %v\n", updates, nil)
	}
	updates, err = scan(ds, "v.io/v23/b")
	if err != nil {
		t.Fatalf("Scan failed: %v\n", err)
	}
	if !match(updates, services[1]) {
		t.Errorf("Scan failed; got %v, but wanted %v\n", updates, services[1])
	}
	// Stop advertising the other. Now shouldn't discover any service.
	stops[1]()
	updates, err = scan(ds, "")
	if err != nil {
		t.Fatalf("Scan failed: %v\n", err)
	}
	if !match(updates) {
		t.Errorf("Scan failed; got %v, but wanted %v\n", updates, nil)
	}
}

func advertise(ds discovery.Advertiser, services ...discovery.Service) (func(), error) {
	ctx, cancel := context.RootContext()
	for _, service := range services {
		if err := ds.Advertise(ctx, service, nil); err != nil {
			return nil, err
		}
	}
	return cancel, nil
}

func scan(ds discovery.Scanner, query string) ([]discovery.Update, error) {
	ctx, cancel := context.RootContext()
	defer cancel()
	updateCh, err := ds.Scan(ctx, query)
	if err != nil {
		return nil, err
	}
	var updates []discovery.Update
	for {
		select {
		case update := <-updateCh:
			updates = append(updates, update)
		case <-time.After(10 * time.Millisecond):
			return updates, nil
		}
	}
}

func match(updates []discovery.Update, wants ...discovery.Service) bool {
	for _, want := range wants {
		matched := false
		for i, update := range updates {
			found, ok := update.(discovery.UpdateFound)
			if !ok {
				continue
			}
			matched = reflect.DeepEqual(found.Value.Service, want)
			if matched {
				updates = append(updates[:i], updates[i+1:]...)
				break
			}
		}
		if !matched {
			return false
		}
	}
	return len(updates) == 0
}
