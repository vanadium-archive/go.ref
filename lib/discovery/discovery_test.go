// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"testing"
	"time"

	"v.io/v23/discovery"
	"v.io/v23/security"

	"v.io/x/lib/ibe"
	idiscovery "v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/plugins/mock"
	"v.io/x/ref/lib/security/bcrypter"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

func TestBasic(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	df, err := idiscovery.NewFactory(ctx, mock.New())
	if err != nil {
		t.Fatal(err)
	}
	defer df.Shutdown()

	services := []discovery.Service{
		{
			InstanceId:    "123",
			InterfaceName: "v.io/v23/a",
			Attrs:         discovery.Attributes{"a1": "v1"},
			Addrs:         []string{"/h1:123/x", "/h2:123/y"},
		},
		{
			InterfaceName: "v.io/v23/b",
			Attrs:         discovery.Attributes{"b1": "v1"},
			Addrs:         []string{"/h1:123/x", "/h2:123/z"},
		},
	}

	d1, err := df.New()
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
	d2, err := df.New()
	if err != nil {
		t.Fatal(err)
	}

	if err := scanAndMatch(ctx, d2, "v.io/v23/a", services[0]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, d2, "v.io/v23/b", services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, d2, "", services...); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, d2, "v.io/v23/c"); err != nil {
		t.Error(err)
	}

	// Open a new scan channel and consume expected advertisements first.
	scan, scanStop, err := startScan(ctx, d2, "v.io/v23/a")
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

	update = <-scan
	if !matchLost([]discovery.Update{update}, services[0]) {
		t.Errorf("unexpected scan: %v", update)
	}

	// Also it shouldn't affect the other.
	if err := scanAndMatch(ctx, d2, "v.io/v23/b", services[1]); err != nil {
		t.Error(err)
	}

	// Stop advertising the remaining one; Shouldn't discover any service.
	stops[1]()
	if err := scanAndMatch(ctx, d2, ""); err != nil {
		t.Error(err)
	}
}

// TODO(jhahn): Add a low level test that ensures the advertisement is unusable
// by the listener, if encrypted rather than replying on a higher level API.
func TestVisibility(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	master, err := ibe.SetupBB2()
	if err != nil {
		ctx.Fatalf("ibe.SetupBB2 failed: %v", err)
	}

	root := bcrypter.NewRoot("v.io", master)
	crypter := bcrypter.NewCrypter()
	if err := crypter.AddParams(ctx, root.Params()); err != nil {
		ctx.Fatalf("AddParams failed: %v", err)
	}
	sctx := bcrypter.WithCrypter(ctx, crypter)

	df, _ := idiscovery.NewFactory(sctx, mock.New())
	defer df.Shutdown()

	service := discovery.Service{
		InterfaceName: "v.io/v23/a",
		Attrs:         discovery.Attributes{"a1": "v1", "a2": "v2"},
		Addrs:         []string{"/h1:123/x", "/h2:123/y"},
	}
	visibility := []security.BlessingPattern{
		security.BlessingPattern("v.io:bob"),
		security.BlessingPattern("v.io:alice").MakeNonExtendable(),
	}

	d1, _ := df.New()
	stop, err := advertise(sctx, d1, visibility, &service)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	d2, _ := df.New()

	// Bob and his friend should discover the advertisement.
	if ctx, err := withDerivedCrypter(ctx, root, "v.io:bob"); err != nil {
		t.Error(err)
	} else if err := scanAndMatch(ctx, d2, "v.io/v23/a", service); err != nil {
		t.Error(err)
	}
	if ctx, err = withDerivedCrypter(ctx, root, "v.io:bob:friend"); err != nil {
		t.Error(err)
	} else if err := scanAndMatch(ctx, d2, "v.io/v23/a", service); err != nil {
		t.Error(err)
	}

	// Alice should discover the advertisement, but her friend shouldn't.
	if ctx, err = withDerivedCrypter(ctx, root, "v.io:alice"); err != nil {
		t.Error(err)
	} else if err := scanAndMatch(ctx, d2, "v.io/v23/a", service); err != nil {
		t.Error(err)
	}
	if ctx, err = withDerivedCrypter(ctx, root, "v.io:alice:friend"); err != nil {
		t.Error(err)
	} else if err := scanAndMatch(ctx, d2, "v.io/v23/a"); err != nil {
		t.Error(err)
	}

	// Other people shouldn't discover the advertisement.
	if ctx, err = withDerivedCrypter(ctx, root, "v.io:carol"); err != nil {
		t.Error(err)
	} else if err := scanAndMatch(ctx, d2, "v.io/v23/a"); err != nil {
		t.Error(err)
	}
}

func TestDuplicates(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	df, _ := idiscovery.NewFactory(ctx, mock.New())
	defer df.Shutdown()

	service := discovery.Service{
		InstanceId:    "123",
		InterfaceName: "v.io/v23/a",
		Addrs:         []string{"/h1:123/x"},
	}

	d, _ := df.New()
	if _, err := advertise(ctx, d, nil, &service); err != nil {
		t.Fatal(err)
	}
	if _, err := advertise(ctx, d, nil, &service); err == nil {
		t.Error("expect an error; but got none")
	}
}

func TestMerge(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	p1, p2 := mock.New(), mock.New()
	df, _ := idiscovery.NewFactory(ctx, p1, p2)
	defer df.Shutdown()

	ad := idiscovery.Advertisement{
		Service: discovery.Service{
			InstanceId:    "123",
			InterfaceName: "v.io/v23/a",
			Addrs:         []string{"/h1:123/x"},
		},
		Hash: []byte{1, 2, 3},
	}

	d, _ := df.New()
	scan, scanStop, err := startScan(ctx, d, "v.io/v23/a")
	if err != nil {
		t.Fatal(err)
	}
	defer scanStop()

	// A plugin returns an advertisement and we should see it.
	p1.RegisterAdvertisement(ad)
	update := <-scan
	if !matchFound([]discovery.Update{update}, ad.Service) {
		t.Errorf("unexpected scan: %v", update)
	}

	// The other plugin returns the same advertisement, but we should not see it.
	p2.RegisterAdvertisement(ad)
	select {
	case update = <-scan:
		t.Errorf("unexpected scan: %v", update)
	case <-time.After(5 * time.Millisecond):
	}

	// Two plugins update the service, but we should see the update only once.
	newAd := ad
	newAd.Service.Addrs = []string{"/h1:456/x"}
	newAd.Hash = []byte{4, 5, 6}

	go func() { p1.RegisterAdvertisement(newAd) }()
	go func() { p2.RegisterAdvertisement(newAd) }()

	// Should see 'Lost' first.
	update = <-scan
	if !matchLost([]discovery.Update{update}, ad.Service) {
		t.Errorf("unexpected scan: %v", update)
	}
	update = <-scan
	if !matchFound([]discovery.Update{update}, newAd.Service) {
		t.Errorf("unexpected scan: %v", update)
	}
	select {
	case update = <-scan:
		t.Errorf("unexpected scan: %v", update)
	case <-time.After(5 * time.Millisecond):
	}
}

func TestShutdown(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	df, _ := idiscovery.NewFactory(ctx, mock.New())

	service := discovery.Service{
		InterfaceName: "v.io/v23/a",
		Addrs:         []string{"/h1:123/x"},
	}

	d1, _ := df.New()
	if _, err := advertise(ctx, d1, nil, &service); err != nil {
		t.Error(err)
	}
	d2, _ := df.New()
	if err := scanAndMatch(ctx, d2, "", service); err != nil {
		t.Error(err)
	}

	// Verify Close can be called multiple times.
	df.Shutdown()
	df.Shutdown()

	// Make sure advertise and scan do not work after closed.
	service.InstanceId = "" // To avoid dup error.
	if _, err := advertise(ctx, d1, nil, &service); err == nil {
		t.Error("expect an error; but got none")
	}
	if err := scanAndMatch(ctx, d2, "", service); err == nil {
		t.Error("expect an error; but got none")
	}
}
