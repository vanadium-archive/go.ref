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

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"

	ldiscovery "v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/plugins/mock"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

func advertise(ctx *context.T, ds discovery.Advertiser, perms []security.BlessingPattern, services ...discovery.Service) (func(), error) {
	ctx, stop := context.WithCancel(ctx)
	for _, service := range services {
		if err := ds.Advertise(ctx, service, perms); err != nil {
			return nil, fmt.Errorf("Advertise failed: %v", err)
		}
	}
	return stop, nil
}

func startScan(ctx *context.T, ds discovery.Scanner, query string) (<-chan discovery.Update, func(), error) {
	ctx, stop := context.WithCancel(ctx)
	scan, err := ds.Scan(ctx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("Scan failed: %v", err)
	}
	return scan, stop, err
}

func scan(ctx *context.T, ds discovery.Scanner, query string) ([]discovery.Update, error) {
	scan, stop, err := startScan(ctx, ds, query)
	if err != nil {
		return nil, err
	}
	defer stop()

	var updates []discovery.Update
	for {
		select {
		case update := <-scan:
			updates = append(updates, update)
		case <-time.After(5 * time.Millisecond):
			return updates, nil
		}
	}
}

func scanAndMatch(ctx *context.T, ds discovery.Scanner, query string, wants ...discovery.Service) error {
	const timeout = 3 * time.Second

	var updates []discovery.Update
	for now := time.Now(); time.Since(now) < timeout; {
		runtime.Gosched()

		var err error
		updates, err = scan(ctx, ds, query)
		if err != nil {
			return err
		}
		if matchFound(updates, wants...) {
			return nil
		}
	}
	return fmt.Errorf("Match failed; got %v, but wanted %v", updates, wants)
}

func match(updates []discovery.Update, lost bool, wants ...discovery.Service) bool {
	for _, want := range wants {
		matched := false
		for i, update := range updates {
			var service discovery.Service
			switch u := update.(type) {
			case discovery.UpdateFound:
				if !lost {
					service = u.Value.Service
				}
			case discovery.UpdateLost:
				if lost {
					service = u.Value.Service
				}
			}
			matched = reflect.DeepEqual(service, want)
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

func matchFound(updates []discovery.Update, wants ...discovery.Service) bool {
	return match(updates, false, wants...)
}

func matchLost(updates []discovery.Update, wants ...discovery.Service) bool {
	return match(updates, true, wants...)
}

func TestBasic(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	ds := ldiscovery.New([]ldiscovery.Plugin{mock.New()})
	services := []discovery.Service{
		{
			InstanceUuid:  ldiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/v23/a",
			Attrs:         discovery.Attributes{"a1": "v1"},
			Addrs:         []string{"/h1:123/x", "/h2:123/y"},
		},
		{
			InstanceUuid:  ldiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/v23/b",
			Attrs:         discovery.Attributes{"b1": "v1"},
			Addrs:         []string{"/h1:123/x", "/h2:123/z"},
		},
	}
	var stops []func()
	for _, service := range services {
		stop, err := advertise(ctx, ds, nil, service)
		if err != nil {
			t.Fatal(err)
		}
		stops = append(stops, stop)
	}

	// Make sure all advertisements are discovered.
	if err := scanAndMatch(ctx, ds, "v.io/v23/a", services[0]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, ds, "v.io/v23/b", services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, ds, "", services...); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, ds, "v.io/v23/c"); err != nil {
		t.Error(err)
	}

	// Open a new scan channel and consume expected advertisements first.
	scan, scanStop, err := startScan(ctx, ds, "v.io/v23/a")
	if err != nil {
		t.Error(err)
	}
	defer scanStop()
	update := <-scan
	if !matchFound([]discovery.Update{update}, services[0]) {
		t.Errorf("Unexpected scan: %v", update)
	}

	// Make sure scan returns the lost advertisement when advertising is stopped.
	stops[0]()

	update = <-scan
	if !matchLost([]discovery.Update{update}, services[0]) {
		t.Errorf("Unexpected scan: %v", update)
	}

	// Also it shouldn't affect the other.
	if err := scanAndMatch(ctx, ds, "v.io/v23/b", services[1]); err != nil {
		t.Error(err)
	}

	// Stop advertising the remaining one; Shouldn't discover any service.
	stops[1]()
	if err := scanAndMatch(ctx, ds, ""); err != nil {
		t.Error(err)
	}
}

// TODO(jhahn): Add a low level test that ensures the advertisement is unusable
// by the listener, if encrypted rather than replying on a higher level API.
func TestPermission(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	ds := ldiscovery.New([]ldiscovery.Plugin{mock.New()})
	service := discovery.Service{
		InstanceUuid:  ldiscovery.NewInstanceUUID(),
		InterfaceName: "v.io/v23/a",
		Attrs:         discovery.Attributes{"a1": "v1", "a2": "v2"},
		Addrs:         []string{"/h1:123/x", "/h2:123/y"},
	}
	perms := []security.BlessingPattern{
		security.BlessingPattern("v.io/bob"),
		security.BlessingPattern("v.io/alice").MakeNonExtendable(),
	}
	stop, err := advertise(ctx, ds, perms, service)
	defer stop()
	if err != nil {
		t.Fatal(err)
	}

	// Bob and his friend should discover the advertisement.
	ctx, _ = v23.WithPrincipal(ctx, testutil.NewPrincipal("v.io/bob"))
	if err := scanAndMatch(ctx, ds, "v.io/v23/a", service); err != nil {
		t.Error(err)
	}
	ctx, _ = v23.WithPrincipal(ctx, testutil.NewPrincipal("v.io/bob/friend"))
	if err := scanAndMatch(ctx, ds, "v.io/v23/a", service); err != nil {
		t.Error(err)
	}

	// Alice should discover the advertisement, but her friend shouldn't.
	ctx, _ = v23.WithPrincipal(ctx, testutil.NewPrincipal("v.io/alice"))
	if err := scanAndMatch(ctx, ds, "v.io/v23/a", service); err != nil {
		t.Error(err)
	}
	ctx, _ = v23.WithPrincipal(ctx, testutil.NewPrincipal("v.io/alice/friend"))
	if err := scanAndMatch(ctx, ds, "v.io/v23/a"); err != nil {
		t.Error(err)
	}

	// Other people shouldn't discover the advertisement.
	ctx, _ = v23.WithPrincipal(ctx, testutil.NewPrincipal("v.io/carol"))
	if err := scanAndMatch(ctx, ds, "v.io/v23/a"); err != nil {
		t.Error(err)
	}
}