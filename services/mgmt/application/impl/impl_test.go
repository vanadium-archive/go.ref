package impl_test

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/verror"

	"v.io/core/veyron/lib/testutil"
	_ "v.io/core/veyron/profiles/static"
	//vsecurity "v.io/core/veyron/security"
	"v.io/core/veyron/services/mgmt/application/impl"
	mgmttest "v.io/core/veyron/services/mgmt/lib/testutil"
	"v.io/core/veyron/services/mgmt/repository"
)

func newPublisherSignature(t *testing.T, ctx *context.T, msg []byte) (security.WireBlessings, security.Signature) {
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
	return security.MarshalBlessings(b), sig
}

// TestInterface tests that the implementation correctly implements
// the Application interface.
func TestInterface(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	dir, prefix := "", ""
	store, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("TempDir(%q, %q) failed: %v", dir, prefix, err)
	}
	defer os.RemoveAll(store)
	dispatcher, err := impl.NewDispatcher(store)
	if err != nil {
		t.Fatalf("impl.NewDispatcher() failed: %v", err)
	}

	server, endpoint := mgmttest.NewServer(ctx)
	defer server.Stop()

	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	// Create client stubs for talking to the server.
	stub := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search"))
	stubV1 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v1"))
	stubV2 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v2"))

	blessings, sig := newPublisherSignature(t, ctx, []byte("binarycontents"))

	// Create example envelopes.
	envelopeV1 := application.Envelope{
		Args: []string{"--help"},
		Env:  []string{"DEBUG=1"},
		Binary: application.SignedFile{
			File:      "/veyron/name/of/binary",
			Signature: sig,
		},
		Publisher: blessings,
	}
	envelopeV2 := application.Envelope{
		Args: []string{"--verbose"},
		Env:  []string{"DEBUG=0"},
		Binary: application.SignedFile{
			File:      "/veyron/name/of/binary",
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
	if err := stub.Put(ctx, []string{"base", "media"}, envelopeV1); err == nil || !verror.Is(err, impl.ErrInvalidSuffix.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrInvalidSuffix, err)
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
	if _, err := stubV2.Match(ctx, []string{"media"}); err == nil || !verror.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if _, err := stubV2.Match(ctx, []string{}); err == nil || !verror.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if _, err := stub.Match(ctx, []string{"media"}); err == nil || !verror.Is(err, impl.ErrInvalidSuffix.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrInvalidSuffix, err)
	}

	// Test Glob
	matches, err := testutil.GlobName(ctx, naming.JoinAddressName(endpoint, ""), "...")
	if err != nil {
		t.Errorf("Unexpected Glob error: %v", err)
	}
	expected := []string{
		"",
		"search",
		"search/v1",
		"search/v2",
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
	if err := stubV1.Remove(ctx, "base"); err == nil || !verror.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if err := stub.Remove(ctx, "base"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}
	if err := stubV2.Remove(ctx, "media"); err == nil || !verror.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if err := stubV1.Remove(ctx, "media"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Finally, use Match() to test that Remove really removed the
	// application envelopes.
	if _, err := stubV1.Match(ctx, []string{"base"}); err == nil || !verror.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if _, err := stubV1.Match(ctx, []string{"media"}); err == nil || !verror.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if _, err := stubV2.Match(ctx, []string{"base"}); err == nil || !verror.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}

	// Shutdown the application repository server.
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

func TestPreserveAcrossRestarts(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	dir, prefix := "", ""
	storedir, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("TempDir(%q, %q) failed: %v", dir, prefix, err)
	}
	defer os.RemoveAll(storedir)

	dispatcher, err := impl.NewDispatcher(storedir)
	if err != nil {
		t.Fatalf("impl.NewDispatcher() failed: %v", err)
	}

	server, endpoint := mgmttest.NewServer(ctx)

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
			File:      "/veyron/name/of/binary",
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
	dispatcher, err = impl.NewDispatcher(storedir)
	if err != nil {
		t.Fatalf("impl.NewDispatcher() failed: %v", err)
	}

	server, endpoint = mgmttest.NewServer(ctx)
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
