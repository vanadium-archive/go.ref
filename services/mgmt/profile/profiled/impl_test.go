// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/services/mgmt/build"

	"v.io/x/ref/services/mgmt/profile"
	"v.io/x/ref/services/mgmt/repository"
	"v.io/x/ref/test"
)

var (
	// spec is an example profile specification used throughout the test.
	spec = profile.Specification{
		Arch:        build.AMD64,
		Description: "Example profile to test the profile repository implementation.",
		Format:      build.ELF,
		Libraries:   map[profile.Library]struct{}{profile.Library{Name: "foo", MajorVersion: "1", MinorVersion: "0"}: struct{}{}},
		Label:       "example",
		Os:          build.Linux,
	}
)

//go:generate v23 test generate

// TestInterface tests that the implementation correctly implements
// the Profile interface.
func TestInterface(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	// Setup and start the profile repository server.
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dir, prefix := "", ""
	store, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("TempDir(%q, %q) failed: %v", dir, prefix, err)
	}
	defer os.RemoveAll(store)
	dispatcher, err := NewDispatcher(store, nil)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}
	l := v23.GetListenSpec(ctx)
	endpoints, err := server.Listen(l)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", l, err)
	}
	endpoint := endpoints[0]
	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	t.Logf("Profile repository at %v", endpoint)

	// Create client stubs for talking to the server.
	stub := repository.ProfileClient(naming.JoinAddressName(endpoint.String(), "linux/base"))

	// Put
	if err := stub.Put(ctx, spec); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// Label
	label, err := stub.Label(ctx)
	if err != nil {
		t.Fatalf("Label() failed: %v", err)
	}
	if label != spec.Label {
		t.Fatalf("Unexpected output: expected %v, got %v", spec.Label, label)
	}

	// Description
	description, err := stub.Description(ctx)
	if err != nil {
		t.Fatalf("Description() failed: %v", err)
	}
	if description != spec.Description {
		t.Fatalf("Unexpected output: expected %v, got %v", spec.Description, description)
	}

	// Specification
	specification, err := stub.Specification(ctx)
	if err != nil {
		t.Fatalf("Specification() failed: %v", err)
	}
	if !reflect.DeepEqual(spec, specification) {
		t.Fatalf("Unexpected output: expected %v, got %v", spec, specification)
	}

	// Remove
	if err := stub.Remove(ctx); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Shutdown the content manager server.
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

func TestPreserveAcrossRestarts(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	// Setup and start the profile repository server.
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dir, prefix := "", ""
	storedir, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("TempDir(%q, %q) failed: %v", dir, prefix, err)
	}
	defer os.RemoveAll(storedir)

	dispatcher, err := NewDispatcher(storedir, nil)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}
	l := v23.GetListenSpec(ctx)
	endpoints, err := server.Listen(l)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", l, err)
	}
	endpoint := endpoints[0]
	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	t.Logf("Profile repository at %v", endpoint)

	// Create client stubs for talking to the server.
	stub := repository.ProfileClient(naming.JoinAddressName(endpoint.String(), "linux/base"))

	if err := stub.Put(ctx, spec); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	label, err := stub.Label(ctx)
	if err != nil {
		t.Fatalf("Label() failed: %v", err)
	}
	if label != spec.Label {
		t.Fatalf("Unexpected output: expected %v, got %v", spec.Label, label)
	}

	// Stop the first server.
	server.Stop()

	// Setup and start a second server.
	server, err = v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dispatcher, err = NewDispatcher(storedir, nil)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}
	endpoints, err = server.Listen(l)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", l, err)
	}
	if err = server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	// Create client stubs for talking to the server.
	stub = repository.ProfileClient(naming.JoinAddressName(endpoints[0].String(), "linux/base"))

	// Label
	label, err = stub.Label(ctx)
	if err != nil {
		t.Fatalf("Label() failed: %v", err)
	}
	if label != spec.Label {
		t.Fatalf("Unexpected output: expected %v, got %v", spec.Label, label)
	}
}
