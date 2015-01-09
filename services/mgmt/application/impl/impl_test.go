package impl_test

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/verror2"

	"v.io/core/veyron/lib/testutil"
	_ "v.io/core/veyron/profiles/static"
	"v.io/core/veyron/services/mgmt/application/impl"
	mgmttest "v.io/core/veyron/services/mgmt/lib/testutil"
	"v.io/core/veyron/services/mgmt/repository"
)

// TestInterface tests that the implementation correctly implements
// the Application interface.
func TestInterface(t *testing.T) {
	ctx := globalRT.NewContext()

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

	server, endpoint := mgmttest.NewServer(globalRT)
	defer server.Stop()

	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	// Create client stubs for talking to the server.
	stub := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search"))
	stubV1 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v1"))
	stubV2 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v2"))

	// Create example envelopes.
	envelopeV1 := application.Envelope{
		Args:   []string{"--help"},
		Env:    []string{"DEBUG=1"},
		Binary: "/veyron/name/of/binary",
	}
	envelopeV2 := application.Envelope{
		Args:   []string{"--verbose"},
		Env:    []string{"DEBUG=0"},
		Binary: "/veyron/name/of/binary",
	}

	// Test Put(), adding a number of application envelopes.
	if err := stubV1.Put(ctx, []string{"base", "media"}, envelopeV1); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if err := stubV2.Put(ctx, []string{"base"}, envelopeV2); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if err := stub.Put(ctx, []string{"base", "media"}, envelopeV1); err == nil || !verror2.Is(err, impl.ErrInvalidSuffix.ID) {
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
	if _, err := stubV2.Match(ctx, []string{"media"}); err == nil || !verror2.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if _, err := stubV2.Match(ctx, []string{}); err == nil || !verror2.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if _, err := stub.Match(ctx, []string{"media"}); err == nil || !verror2.Is(err, impl.ErrInvalidSuffix.ID) {
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
	if err := stubV1.Remove(ctx, "base"); err == nil || !verror2.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if err := stub.Remove(ctx, "base"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}
	if err := stubV2.Remove(ctx, "media"); err == nil || !verror2.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if err := stubV1.Remove(ctx, "media"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Finally, use Match() to test that Remove really removed the
	// application envelopes.
	if _, err := stubV1.Match(ctx, []string{"base"}); err == nil || !verror2.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if _, err := stubV1.Match(ctx, []string{"media"}); err == nil || !verror2.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}
	if _, err := stubV2.Match(ctx, []string{"base"}); err == nil || !verror2.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", impl.ErrNotFound, err)
	}

	// Shutdown the application repository server.
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

func TestPreserveAcrossRestarts(t *testing.T) {
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

	server, endpoint := mgmttest.NewServer(globalRT)
	defer server.Stop()

	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	// Create client stubs for talking to the server.
	stubV1 := repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v1"))

	// Create example envelopes.
	envelopeV1 := application.Envelope{
		Args:   []string{"--help"},
		Env:    []string{"DEBUG=1"},
		Binary: "/veyron/name/of/binary",
	}

	if err := stubV1.Put(globalRT.NewContext(), []string{"media"}, envelopeV1); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// There is content here now.
	output, err := stubV1.Match(globalRT.NewContext(), []string{"media"})
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

	server, endpoint = mgmttest.NewServer(globalRT)
	defer server.Stop()

	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	stubV1 = repository.ApplicationClient(naming.JoinAddressName(endpoint, "search/v1"))

	output, err = stubV1.Match(globalRT.NewContext(), []string{"media"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "media", err)
	}
	if !reflect.DeepEqual(envelopeV1, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV1, output)
	}
}
