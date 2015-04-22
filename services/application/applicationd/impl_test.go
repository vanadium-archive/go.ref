// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/application"
	"v.io/v23/verror"

	appd "v.io/x/ref/services/application/applicationd"
	"v.io/x/ref/services/internal/servicetest"
	"v.io/x/ref/services/repository"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

func newPublisherSignature(t *testing.T, ctx *context.T, msg []byte) (security.Blessings, security.Signature) {
	// Generate publisher blessings
	p := v23.GetPrincipal(ctx)
	b, err := p.BlessSelf("publisher")
	if err != nil {
		t.Fatal(err)
	}
	sig, err := p.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}
	return b, sig
}

// TestInterface tests that the implementation correctly implements
// the Application interface.
func TestInterface(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	dir, prefix := "", ""
	store, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("TempDir(%q, %q) failed: %v", dir, prefix, err)
	}
	defer os.RemoveAll(store)
	dispatcher, err := appd.NewDispatcher(store)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}

	server, endpoint := servicetest.NewServer(ctx)
	defer server.Stop()

	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	// Create client stubs for talking to the server.
	stub := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search"))
	stubV0 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v0"))
	stubV1 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v1"))
	stubV2 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v2"))
	stubV3 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v3"))

	blessings, sig := newPublisherSignature(t, ctx, []byte("binarycontents"))

	// Create example envelopes.
	envelopeV1 := application.Envelope{
		Args: []string{"--help"},
		Env:  []string{"DEBUG=1"},
		Binary: application.SignedFile{
			File:      "/v23/name/of/binary",
			Signature: sig,
		},
		Publisher: blessings,
	}
	envelopeV2 := application.Envelope{
		Args: []string{"--verbose"},
		Env:  []string{"DEBUG=0"},
		Binary: application.SignedFile{
			File:      "/v23/name/of/binary",
			Signature: sig,
		},
		Publisher: blessings,
	}
	envelopeV3 := application.Envelope{
		Args: []string{"--verbose", "--spiffynewflag"},
		Env:  []string{"DEBUG=0"},
		Binary: application.SignedFile{
			File:      "/v23/name/of/binary",
			Signature: sig,
		},
		Publisher: blessings,
	}

	// Test Put(), adding a number of application envelopes.
	if err := stubV1.Put(ctx, []string{"base", "media"}, envelopeV1); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if err := stubV2.Put(ctx, []string{"base"}, envelopeV2); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if err := stub.Put(ctx, []string{"base", "media"}, envelopeV1); err == nil || verror.ErrorID(err) != appd.ErrInvalidSuffix.ID {
		t.Fatalf("Unexpected error: expected %v, got %v", appd.ErrInvalidSuffix, err)
	}

	// Test Match(), trying to retrieve both existing and non-existing
	// application envelopes.
	var output application.Envelope
	if output, err = stubV2.Match(ctx, []string{"base", "media"}); err != nil {
		t.Fatalf("Match() failed: %v", err)
	}
	if !reflect.DeepEqual(envelopeV2, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV2, output)
	}
	if output, err = stubV1.Match(ctx, []string{"media"}); err != nil {
		t.Fatalf("Match() failed: %v", err)
	}
	if !reflect.DeepEqual(envelopeV1, output) {
		t.Fatalf("Unexpected output: expected %v, got %v", envelopeV1, output)
	}
	if _, err := stubV2.Match(ctx, []string{"media"}); err == nil || verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatalf("Unexpected error: expected %v, got %v", verror.ErrNoExist, err)
	}
	if _, err := stubV2.Match(ctx, []string{}); err == nil || verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatalf("Unexpected error: expected %v, got %v", verror.ErrNoExist, err)
	}

	// Test that Match() against a name without a version suffix returns the latest.
	if output, err = stub.Match(ctx, []string{"base", "media"}); err != nil {
		t.Fatalf("Match() failed: %v", err)
	}
	if !reflect.DeepEqual(envelopeV2, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV2, output)
	}

	// Test that we can add another envelope in sort order and we still get the
	// correct (i.e. newest) version.
	if err := stubV3.Put(ctx, []string{"base"}, envelopeV3); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if output, err = stub.Match(ctx, []string{"base", "media"}); err != nil {
		t.Fatalf("Match() failed: %v", err)
	}
	if !reflect.DeepEqual(envelopeV3, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV3, output)
	}

	// Test that this is not based on time but on sort order.
	envelopeV0 := application.Envelope{
		Args: []string{"--help", "--zeroth"},
		Env:  []string{"DEBUG=1"},
		Binary: application.SignedFile{
			File:      "/v23/name/of/binary",
			Signature: sig,
		},
		Publisher: blessings,
	}
	if err := stubV0.Put(ctx, []string{"base"}, envelopeV0); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if output, err = stub.Match(ctx, []string{"base", "media"}); err != nil {
		t.Fatalf("Match() failed: %v", err)
	}
	if !reflect.DeepEqual(envelopeV3, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV3, output)
	}

	// Test Glob
	matches, _, err := testutil.GlobName(ctx, naming.JoinAddressName(endpoint, ""), "...")
	if err != nil {
		t.Errorf("Unexpected Glob error: %v", err)
	}
	expected := []string{
		"",
		"search",
		"search/v0",
		"search/v1",
		"search/v2",
		"search/v3",
	}
	if !reflect.DeepEqual(matches, expected) {
		t.Errorf("unexpected Glob results. Got %q, want %q", matches, expected)
	}

	// Test Remove(), trying to remove both existing and non-existing
	// application envelopes.
	if err := stubV1.Remove(ctx, "base"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}
	if output, err = stubV1.Match(ctx, []string{"media"}); err != nil {
		t.Fatalf("Match() failed: %v", err)
	}
	if err := stubV1.Remove(ctx, "base"); err == nil || verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatalf("Unexpected error: expected %v, got %v", verror.ErrNoExist, err)
	}
	if err := stub.Remove(ctx, "base"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}
	if err := stubV2.Remove(ctx, "media"); err == nil || verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatalf("Unexpected error: expected %v, got %v", verror.ErrNoExist, err)
	}
	if err := stubV1.Remove(ctx, "media"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Finally, use Match() to test that Remove really removed the
	// application envelopes.
	if _, err := stubV1.Match(ctx, []string{"base"}); err == nil || verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatalf("Unexpected error: expected %v, got %v", verror.ErrNoExist, err)
	}
	if _, err := stubV1.Match(ctx, []string{"media"}); err == nil || verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatalf("Unexpected error: expected %v, got %v", verror.ErrNoExist, err)
	}
	if _, err := stubV2.Match(ctx, []string{"base"}); err == nil || verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatalf("Unexpected error: expected %v, got %v", verror.ErrNoExist, err)
	}

	// Shutdown the application repository server.
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

func TestPreserveAcrossRestarts(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	dir, prefix := "", ""
	storedir, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("TempDir(%q, %q) failed: %v", dir, prefix, err)
	}
	defer os.RemoveAll(storedir)

	dispatcher, err := appd.NewDispatcher(storedir)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}

	server, endpoint := servicetest.NewServer(ctx)

	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	// Create client stubs for talking to the server.
	stubV1 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v1"))

	blessings, sig := newPublisherSignature(t, ctx, []byte("binarycontents"))

	// Create example envelopes.
	envelopeV1 := application.Envelope{
		Args: []string{"--help"},
		Env:  []string{"DEBUG=1"},
		Binary: application.SignedFile{
			File:      "/v23/name/of/binary",
			Signature: sig,
		},
		Publisher: blessings,
	}

	if err := stubV1.Put(ctx, []string{"media"}, envelopeV1); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// There is content here now.
	output, err := stubV1.Match(ctx, []string{"media"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "media", err)
	}
	if !reflect.DeepEqual(envelopeV1, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV1, output)
	}

	server.Stop()

	// Setup and start a second application server.
	dispatcher, err = appd.NewDispatcher(storedir)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}

	server, endpoint = servicetest.NewServer(ctx)
	defer server.Stop()

	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	stubV1 = repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v1"))

	output, err = stubV1.Match(ctx, []string{"media"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "media", err)
	}
	if !reflect.DeepEqual(envelopeV1, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV1, output)
	}
}
