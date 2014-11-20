package impl

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/build"

	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/services/mgmt/profile"
	"veyron.io/veyron/veyron/services/mgmt/repository"
)

var (
	// spec is an example profile specification used throughout the test.
	spec = profile.Specification{
		Arch:        build.AMD64,
		Description: "Example profile to test the profile repository implementation.",
		Format:      build.ELF,
		Libraries:   map[profile.Library]struct{}{profile.Library{Name: "foo", MajorVersion: "1", MinorVersion: "0"}: struct{}{}},
		Label:       "example",
		OS:          build.Linux,
	}
)

// TestInterface tests that the implementation correctly implements
// the Profile interface.
func TestInterface(t *testing.T) {
	runtime := rt.R()
	ctx := runtime.NewContext()

	// Setup and start the profile repository server.
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
	dispatcher, err := NewDispatcher(store, nil)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}
	endpoint, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
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

func init() {
	rt.Init()
}

func TestPreserveAcrossRestarts(t *testing.T) {
	runtime := rt.R()
	ctx := runtime.NewContext()

	// Setup and start the profile repository server.
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
	endpoint, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
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
	server, err = runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	dispatcher, err = NewDispatcher(storedir, nil)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}
	endpoint, err = server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
	if err = server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	// Create client stubs for talking to the server.
	stub = repository.ProfileClient(naming.JoinAddressName(endpoint.String(), "linux/base"))

	// Label
	label, err = stub.Label(ctx)
	if err != nil {
		t.Fatalf("Label() failed: %v", err)
	}
	if label != spec.Label {
		t.Fatalf("Unexpected output: expected %v, got %v", spec.Label, label)
	}
}
