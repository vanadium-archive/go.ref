// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"fmt"
	"reflect"
	"runtime"
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
			t.Fatal(err)
		}
		stops = append(stops, stop)
	}

	if err := scanAndMatch(ds, "v.io/v23/a", services[0]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ds, "v.io/v23/b", services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ds, "", services...); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ds, "v.io/v23/c"); err != nil {
		t.Error(err)
	}

	// Stop advertising the first service. Shouldn't affect the other.
	stops[0]()
	if err := scanAndMatch(ds, "v.io/v23/a"); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ds, "v.io/v23/b", services[1]); err != nil {
		t.Error(err)
	}

	// Stop advertising the other. Now shouldn't discover any service.
	stops[1]()
	if err := scanAndMatch(ds, ""); err != nil {
		t.Error(err)
	}
}

func advertise(ds discovery.Advertiser, services ...discovery.Service) (func(), error) {
	ctx, cancel := context.RootContext()
	for _, service := range services {
		if err := ds.Advertise(ctx, service, nil); err != nil {
			return nil, fmt.Errorf("Advertise failed: %v", err)
		}
	}
	return cancel, nil
}

func scan(ds discovery.Scanner, query string) ([]discovery.Update, error) {
	ctx, _ := context.RootContext()
	updateCh, err := ds.Scan(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("Scan failed: %v", err)
	}
	var updates []discovery.Update
	for {
		select {
		case update := <-updateCh:
			updates = append(updates, update)
		case <-time.After(5 * time.Millisecond):
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

func scanAndMatch(ds discovery.Scanner, query string, wants ...discovery.Service) error {
	const timeout = 3 * time.Second

	var updates []discovery.Update
	for now := time.Now(); time.Since(now) < timeout; {
		runtime.Gosched()

		var err error
		updates, err = scan(ds, query)
		if err != nil {
			return err
		}
		if match(updates, wants...) {
			return nil
		}
	}
	return fmt.Errorf("Match failed; got %v, but wanted %v", updates, wants)
}
