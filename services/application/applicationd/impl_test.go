// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"fmt"
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

	"v.io/x/ref/lib/xrpc"
	appd "v.io/x/ref/services/application/applicationd"
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
	ctx, shutdown := test.V23Init()
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

	server, err := xrpc.NewDispatchingServer(ctx, "", dispatcher)
	if err != nil {
		t.Fatalf("NewServer(%v) failed: %v", dispatcher, err)
	}
	endpoint := server.Status().Endpoints[0].String()

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
	ctx, shutdown := test.V23Init()
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

	server, err := xrpc.NewDispatchingServer(ctx, "", dispatcher)
	if err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}
	endpoint := server.Status().Endpoints[0].String()

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

	server, err = xrpc.NewDispatchingServer(ctx, "", dispatcher)
	if err != nil {
		t.Fatalf("NewServer(%v) failed: %v", dispatcher, err)
	}
	endpoint = server.Status().Endpoints[0].String()

	stubV1 = repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v1"))

	output, err = stubV1.Match(ctx, []string{"media"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "media", err)
	}
	if !reflect.DeepEqual(envelopeV1, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV1, output)
	}
}

// TestTidyNow tests that TidyNow operates correctly.
func TestTidyNow(t *testing.T) {
	ctx, shutdown := test.V23Init()
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

	server, err := xrpc.NewDispatchingServer(ctx, "", dispatcher)
	if err != nil {
		t.Fatalf("NewServer(%v) failed: %v", dispatcher, err)
	}

	defer func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}()
	endpoint := server.Status().Endpoints[0].String()

	// Create client stubs for talking to the server.
	stub := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search"))
	stubs := make([]repository.ApplicationClientStub, 0)
	for _, vn := range []string{"v0", "v1", "v2", "v3"} {
		s := repository.ApplicationClient(naming.JoinAddressName(endpoint, fmt.Sprintf("search/%s", vn)))
		stubs = append(stubs, s)
	}
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

	stuffEnvelopes(t, ctx, stubs, []profEnvTuple{
		{
			&envelopeV1,
			[]string{"base", "media"},
		},
	})

	// Verify that we have one
	testGlob(t, ctx, endpoint, []string{
		"",
		"search",
		"search/v0",
	})

	// Tidy when already tidy does not alter.
	if err := stubs[0].TidyNow(ctx); err != nil {
		t.Errorf("TidyNow failed: %v", err)
	}
	testGlob(t, ctx, endpoint, []string{
		"",
		"search",
		"search/v0",
	})

	stuffEnvelopes(t, ctx, stubs, []profEnvTuple{
		{
			&envelopeV1,
			[]string{"base", "media"},
		},
		{
			&envelopeV2,
			[]string{"media"},
		},
		{
			&envelopeV3,
			[]string{"base"},
		},
	})

	// Now there are three envelopes which is one more than the
	// numberOfVersionsToKeep set for the test. However
	// we need both envelopes v0 and v2 to keep two versions for
	// profile media and envelopes v0 and v3 to keep two versions
	// for profile base so we continue to have three versions.
	if err := stubs[0].TidyNow(ctx); err != nil {
		t.Errorf("TidyNow failed: %v", err)
	}
	testGlob(t, ctx, endpoint, []string{
		"",
		"search",
		"search/v0",
		"search/v1",
		"search/v2",
	})

	// And the newest version for each profile differs because
	// not every version supports all profiles.
	output1, err := stub.Match(ctx, []string{"media"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "media", err)
	}
	if !reflect.DeepEqual(envelopeV2, output1) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV2, output1)
	}

	output2, err := stub.Match(ctx, []string{"base"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "base", err)
	}
	if !reflect.DeepEqual(envelopeV3, output2) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV3, output2)
	}

	// Test that we can add an envelope for v3 with profile media and after calling
	// TidyNow(), there will be all versions still in glob but v0 will only match profile
	// base and not have an envelope for profile media.
	if err := stubs[3].Put(ctx, []string{"media"}, envelopeV3); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	if err := stubs[0].TidyNow(ctx); err != nil {
		t.Errorf("TidyNow failed: %v", err)
	}
	testGlob(t, ctx, endpoint, []string{
		"",
		"search",
		"search/v0",
		"search/v1",
		"search/v2",
		"search/v3",
	})

	output3, err := stubs[0].Match(ctx, []string{"base"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "base", err)
	}
	if !reflect.DeepEqual(envelopeV3, output2) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV3, output3)
	}

	output4, err := stubs[0].Match(ctx, []string{"base"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "base", err)
	}
	if !reflect.DeepEqual(envelopeV3, output2) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV3, output4)
	}

	_, err = stubs[0].Match(ctx, []string{"media"})
	if verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatalf("got error %v,  expected %v", err, verror.ErrNoExist)
	}

	stuffEnvelopes(t, ctx, stubs, []profEnvTuple{
		{
			&envelopeV1,
			[]string{"base", "media"},
		},
		{
			&envelopeV2,
			[]string{"base", "media"},
		},
		{
			&envelopeV3,
			[]string{"base", "media"},
		},
		{
			&envelopeV3,
			[]string{"base", "media"},
		},
	})

	// Now there are four versions for all profiles so tidying
	// will remove the older versions.
	if err := stubs[0].TidyNow(ctx); err != nil {
		t.Errorf("TidyNow failed: %v", err)
	}

	testGlob(t, ctx, endpoint, []string{
		"",
		"search",
		"search/v2",
		"search/v3",
	})
}

type profEnvTuple struct {
	e *application.Envelope
	p []string
}

func testGlob(t *testing.T, ctx *context.T, endpoint string, expected []string) {
	matches, _, err := testutil.GlobName(ctx, naming.JoinAddressName(endpoint, ""), "...")
	if err != nil {
		t.Errorf("Unexpected Glob error: %v", err)
	}
	if !reflect.DeepEqual(matches, expected) {
		t.Errorf("unexpected Glob results. Got %q, want %q", matches, expected)
	}
}

func stuffEnvelopes(t *testing.T, ctx *context.T, stubs []repository.ApplicationClientStub, pets []profEnvTuple) {
	for i, pet := range pets {
		if err := stubs[i].Put(ctx, pet.p, *pet.e); err != nil {
			t.Fatalf("%d: Put(%v) failed: %v", i, pet, err)
		}
	}
}
