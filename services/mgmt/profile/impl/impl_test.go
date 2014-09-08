package impl

import (
	"io/ioutil"
	"reflect"
	"testing"

	"veyron/services/mgmt/profile"
	"veyron/services/mgmt/repository"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/build"
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
	runtime := rt.Init()
	defer runtime.Cleanup()

	ctx := runtime.NewContext()

	// Setup and start the profile repository server.
	server, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()

	// Setup and start the profile server.
	server, err = runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	dir, prefix := "", ""
	store, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("TempDir(%q, %q) failed: %v", dir, prefix, err)
	}
	dispatcher, err := NewDispatcher(store, nil)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}
	protocol, hostname := "tcp", "127.0.0.1:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	if err := server.Serve("", dispatcher); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	t.Logf("Profile repository at %v", endpoint)

	// Create client stubs for talking to the server.
	stub, err := repository.BindProfile(naming.JoinAddressName(endpoint.String(), "//linux/base"))
	if err != nil {
		t.Fatalf("BindApplication() failed: %v", err)
	}

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
