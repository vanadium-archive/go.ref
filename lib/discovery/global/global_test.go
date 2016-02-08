// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"testing"

	"v.io/v23"
	"v.io/v23/discovery"
	"v.io/v23/options"
	"v.io/v23/security"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/test"
	"v.io/x/ref/test/timekeeper"
)

const testPath = "a/b/c"

func TestBasic(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	services := []discovery.Service{
		{
			InstanceId: "123",
			Addrs:      []string{"/h1:123/x", "/h2:123/y"},
		},
		{
			Addrs: []string{"/h1:123/x", "/h2:123/z"},
		},
	}

	d1, err := New(ctx, testPath)
	if err != nil {
		t.Fatal(err)
	}

	var stops []func()
	for i, _ := range services {
		stop, err := advertise(ctx, d1, nil, &services[i])
		if err != nil {
			t.Fatal(err)
		}
		stops = append(stops, stop)
	}

	// Make sure none of advertisements are discoverable by the same discovery instance.
	if err := scanAndMatch(ctx, d1, ""); err != nil {
		t.Error(err)
	}

	// Create a new discovery instance. All advertisements should be discovered with that.
	clock := timekeeper.NewManualTime()
	d2, err := newWithClock(ctx, testPath, clock)
	if err != nil {
		t.Fatal(err)
	}

	if err := scanAndMatch(ctx, d2, "123", services[0]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, d2, services[1].InstanceId, services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, d2, "", services...); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, d2, "_not_exist_"); err != nil {
		t.Error(err)
	}

	// Open a new scan channel and consume expected advertisements first.
	scan, scanStop, err := startScan(ctx, d2, "123")
	if err != nil {
		t.Fatal(err)
	}
	defer scanStop()
	update := <-scan
	if !matchFound([]discovery.Update{update}, services[0]) {
		t.Errorf("unexpected scan: %v", update)
	}

	// Make sure scan returns the lost advertisement when advertising is stopped.
	stops[0]()

	clock.AdvanceTime(scanInterval * 2)
	update = <-scan
	if !matchLost([]discovery.Update{update}, services[0]) {
		t.Errorf("unexpected scan: %v", update)
	}

	// Also it shouldn't affect the other.
	if err := scanAndMatch(ctx, d2, services[1].InstanceId, services[1]); err != nil {
		t.Error(err)
	}

	// Stop advertising the remaining one; Shouldn't discover any service.
	stops[1]()
	if err := scanAndMatch(ctx, d2, ""); err != nil {
		t.Error(err)
	}
}

func TestVisibility(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	service := discovery.Service{
		InstanceId: "123",
		Addrs:      []string{"/h1:123/x", "/h2:123/y"},
	}
	visibility := []security.BlessingPattern{
		security.BlessingPattern("test-blessing:bob"),
		security.BlessingPattern("test-blessing:alice").MakeNonExtendable(),
	}

	d1, _ := New(ctx, testPath)

	mectx, _ := withPrincipal(ctx, "me")
	stop, _ := advertise(mectx, d1, visibility, &service)
	defer stop()

	d2, _ := New(ctx, testPath)

	// Bob and his friend should discover the advertisement.
	bobctx, _ := withPrincipal(ctx, "bob")
	if err := scanAndMatch(bobctx, d2, "123", service); err != nil {
		t.Error(err)
	}
	bobfriendctx, _ := withPrincipal(ctx, "bob:friend")
	if err := scanAndMatch(bobfriendctx, d2, "123", service); err != nil {
		t.Error(err)
	}

	// Alice should discover the advertisement, but her friend shouldn't.
	alicectx, _ := withPrincipal(ctx, "alice")
	if err := scanAndMatch(alicectx, d2, "123", service); err != nil {
		t.Error(err)
	}
	alicefriendctx, _ := withPrincipal(ctx, "alice:friend")
	if err := scanAndMatch(alicefriendctx, d2, "123"); err != nil {
		t.Error(err)
	}

	// Other people shouldn't discover the advertisement.
	carolctx, _ := withPrincipal(ctx, "carol")
	if err := scanAndMatch(carolctx, d2, "123"); err != nil {
		t.Error(err)
	}
}

func TestDuplicates(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	service := discovery.Service{
		InstanceId: "123",
		Addrs:      []string{"/h1:123/x"},
	}

	d, _ := New(ctx, testPath)

	stop, _ := advertise(ctx, d, nil, &service)
	defer stop()

	if _, err := advertise(ctx, d, nil, &service); err == nil {
		t.Error("expect an error; but got none")
	}
}

func TestRefresh(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	clock := timekeeper.NewManualTime()
	mt, err := mounttablelib.NewMountTableDispatcherWithClock(ctx, "", "", "", clock)
	if err != nil {
		t.Fatal(err)
	}
	_, mtserver, err := v23.WithNewDispatchingServer(ctx, "", mt, options.ServesMountTable(true))
	if err != nil {
		t.Fatal(err)
	}
	ns := v23.GetNamespace(ctx)
	ns.SetRoots(mtserver.Status().Endpoints[0].Name())

	service := discovery.Service{
		InstanceId: "123",
		Addrs:      []string{"/h1:123/x"},
	}

	d1, _ := newWithClock(ctx, testPath, clock)

	stop, _ := advertise(ctx, d1, nil, &service)
	defer stop()

	d2, _ := New(ctx, testPath)
	if err := scanAndMatch(ctx, d2, "", service); err != nil {
		t.Error(err)
	}

	// Make sure that the advertisement are refreshed on every ttl time.
	for i := 0; i < 10; i++ {
		clock.AdvanceTime(mountTTL * 2)
		<-clock.Requests()

		if err := scanAndMatch(ctx, d2, "", service); err != nil {
			t.Error(err)
		}
	}
}
