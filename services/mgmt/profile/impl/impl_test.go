package impl

import (
	"reflect"
	"testing"

	"veyron/services/mgmt/profile"
	istore "veyron/services/store/testutil"

	"veyron2/rt"
)

var (
	// spec is an example profile specification used throughout the test.
	spec = profile.Specification{
		Format:      profile.Format{Name: "elf", Attributes: map[string]string{"os": "linux", "arch": "amd64"}},
		Libraries:   map[profile.Library]struct{}{profile.Library{Name: "foo", MajorVersion: "1", MinorVersion: "0"}: struct{}{}},
		Label:       "example",
		Description: "Example profile to test the profile manager implementation.",
	}
)

// TestInterface tests that the implementation correctly implements
// the Profile interface.
func TestInterface(t *testing.T) {
	runtime := rt.Init()
	defer runtime.Shutdown()

	// Setup and start the profile manager server.
	server, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}

	// Setup and start a store server.
	mountPoint, cleanup := istore.NewStore(t, server, runtime.Identity().PublicID())
	defer cleanup()

	dispatcher, err := NewDispatcher(mountPoint)
	if err != nil {
		t.Fatalf("NewDispatcher() failed: %v", err)
	}
	suffix := ""
	if err := server.Register(suffix, dispatcher); err != nil {
		t.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	name := ""
	if err := server.Publish(name); err != nil {
		t.Fatalf("Publish(%v) failed: %v", name, err)
	}
	t.Logf("Profile manager published at %v/%v", endpoint, name)

	// Create client stubs for talking to the server.
	stub, err := profile.BindProfile("/" + endpoint.String() + "/linux/base")
	if err != nil {
		t.Fatalf("BindApplication() failed: %v", err)
	}

	// Put
	if err := stub.Put(spec); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// Label
	label, err := stub.Label()
	if err != nil {
		t.Fatalf("Label() failed: %v", err)
	}
	if label != spec.Label {
		t.Fatalf("Unexpected output: expected %v, got %v", spec.Label, label)
	}

	// Description
	description, err := stub.Description()
	if err != nil {
		t.Fatalf("Description() failed: %v", err)
	}
	if description != spec.Description {
		t.Fatalf("Unexpected output: expected %v, got %v", spec.Description, description)
	}

	// Specification
	specification, err := stub.Specification()
	if err != nil {
		t.Fatalf("Specification() failed: %v", err)
	}
	if !reflect.DeepEqual(spec, specification) {
		t.Fatalf("Unexpected output: expected %v, got %v", spec, specification)
	}

	// Remove
	if err := stub.Remove(); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Shutdown the content manager server.
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}
