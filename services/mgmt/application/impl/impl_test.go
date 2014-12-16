package impl

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/verror2"

	"veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/services/mgmt/repository"
)

// TestInterface tests that the implementation correctly implements
// the Application interface.
func TestInterface(t *testing.T) {
	ctx := runtime.NewContext()

	// Setup and start the application repository server.
	server, err := runtime.NewServer()
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

	endpoints, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}
	endpoint := endpoints[0]

	// Create client stubs for talking to the server.
	stub := repository.ApplicationClient(naming.JoinAddressName(endpoint.String(), "search"))
	stubV1 := repository.ApplicationClient(naming.JoinAddressName(endpoint.String(), "search/v1"))
	stubV2 := repository.ApplicationClient(naming.JoinAddressName(endpoint.String(), "search/v2"))

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
	if err := stub.Put(ctx, []string{"base", "media"}, envelopeV1); err == nil || !verror2.Is(err, errInvalidSuffix.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errInvalidSuffix, err)
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
	if _, err := stubV2.Match(ctx, []string{"media"}); err == nil || !verror2.Is(err, errNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errNotFound, err)
	}
	if _, err := stubV2.Match(ctx, []string{}); err == nil || !verror2.Is(err, errNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errNotFound, err)
	}
	if _, err := stub.Match(ctx, []string{"media"}); err == nil || !verror2.Is(err, errInvalidSuffix.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errInvalidSuffix, err)
	}

	// Test Glob
	matches, err := testutil.GlobName(ctx, naming.JoinAddressName(endpoint.String(), ""), "...")
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
	if err := stubV1.Remove(ctx, "base"); err == nil || !verror2.Is(err, errNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errNotFound, err)
	}
	if err := stub.Remove(ctx, "base"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}
	if err := stubV2.Remove(ctx, "media"); err == nil || !verror2.Is(err, errNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errNotFound, err)
	}
	if err := stubV1.Remove(ctx, "media"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Finally, use Match() to test that Remove really removed the
	// application envelopes.
	if _, err := stubV1.Match(ctx, []string{"base"}); err == nil || !verror2.Is(err, errNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errNotFound, err)
	}
	if _, err := stubV1.Match(ctx, []string{"media"}); err == nil || !verror2.Is(err, errNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errNotFound, err)
	}
	if _, err := stubV2.Match(ctx, []string{"base"}); err == nil || !verror2.Is(err, errNotFound.ID) {
		t.Fatalf("Unexpected error: expected %v, got %v", errNotFound, err)
	}

	// Shutdown the application repository server.
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

var runtime veyron2.Runtime

func init() {
	var err error
	runtime, err = rt.New()
	if err != nil {
		panic(err)
	}
}

func TestPreserveAcrossRestarts(t *testing.T) {
	server, err := runtime.NewServer()
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

	endpoints, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
	endpoint := endpoints[0]
	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	// Create client stubs for talking to the server.
	stubV1 := repository.ApplicationClient(naming.JoinAddressName(endpoint.String(), "search/v1"))

	// Create example envelopes.
	envelopeV1 := application.Envelope{
		Args:   []string{"--help"},
		Env:    []string{"DEBUG=1"},
		Binary: "/veyron/name/of/binary",
	}

	if err := stubV1.Put(runtime.NewContext(), []string{"media"}, envelopeV1); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// There is content here now.
	output, err := stubV1.Match(runtime.NewContext(), []string{"media"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "media", err)
	}
	if !reflect.DeepEqual(envelopeV1, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV1, output)
	}

	server.Stop()

	// Setup and start a second application server in its place.
	server, err = runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dispatcher, err = NewDispatcher(storedir, nil)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}
	endpoints, err = server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
	endpoint = endpoints[0]
	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}

	stubV1 = repository.ApplicationClient(naming.JoinAddressName(endpoint.String(), "search/v1"))

	output, err = stubV1.Match(runtime.NewContext(), []string{"media"})
	if err != nil {
		t.Fatalf("Match(%v) failed: %v", "media", err)
	}
	if !reflect.DeepEqual(envelopeV1, output) {
		t.Fatalf("Incorrect output: expected %v, got %v", envelopeV1, output)
	}
}
