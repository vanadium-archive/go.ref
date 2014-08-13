package impl_test

import (
	"fmt"
	"os"
	"testing"

	mtlib "veyron/services/mounttable/lib"

	"veyron/lib/testutil/blackbox"
	"veyron/lib/testutil/security"
	"veyron/services/mgmt/lib/exec"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/node"
	"veyron2/verror"
	"veyron2/vlog"
)

// TODO(caprita): I've had to write one too many of these, let's move it to some
// central utility library.

// setupLocalNamespace sets up a mounttable and sets the local namespace root
// to point to it.  Returns a cleanup function.
func setupLocalNamespace(t *testing.T) func() {
	server, err := rt.R().NewServer(veyron2.ServesMountTableOpt(true))
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	dispatcher, err := mtlib.NewMountTable("")
	if err != nil {
		t.Fatalf("NewMountTable() failed: %v", err)
	}
	protocol, hostname := "tcp", "127.0.0.1:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	if err := server.Serve("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}
	name := naming.JoinAddressName(endpoint.String(), "")
	vlog.VI(1).Infof("Mount table object name: %v", name)
	ns := rt.R().Namespace()
	// Make the runtime's namespace rooted at the MountTable server started
	// above.
	ns.SetRoots(name)
	return func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
		// The runtime outlives the particular test case that invokes
		// setupLocalNamespace. It's good practice to reset the
		// runtime's state before the next test uses it.
		ns.SetRoots()
	}
}

// TODO(caprita): Move this setup into the blackbox lib.

// setupChildCommand configures the child to use the right mounttable root
// and identity.  It returns a cleanup function.
func setupChildCommand(child *blackbox.Child) func() {
	cmd := child.Cmd
	for i, root := range rt.R().Namespace().Roots() {
		cmd.Env = exec.Setenv(cmd.Env, fmt.Sprintf("NAMESPACE_ROOT%d", i), root)
	}
	idFile := security.SaveIdentityToFile(security.NewBlessedIdentity(rt.R().Identity(), "test"))
	cmd.Env = exec.Setenv(cmd.Env, "VEYRON_IDENTITY", idFile)
	return func() {
		os.Remove(idFile)
	}
}

func newServer() (ipc.Server, string) {
	server, err := rt.R().NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	protocol, hostname := "tcp", "127.0.0.1:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	return server, endpoint.String()
}

// resolveExpectError verifies that the given name is not in the mounttable.
func resolveExpectError(t *testing.T, name string, errID verror.ID) {
	if results, err := rt.R().Namespace().Resolve(rt.R().NewContext(), name); err == nil {
		t.Errorf("Resolve(%v) succeeded with results %v when it was expected to fail", name, results)
	} else if !verror.Is(err, errID) {
		t.Errorf("Resolve(%v) failed with error %v, expected error ID %v", err, errID)
	}
}

// resolve looks up the given name in the mounttable.
func resolve(t *testing.T, name string) string {
	results, err := rt.R().Namespace().Resolve(rt.R().NewContext(), name)
	if err != nil {
		t.Fatalf("Resolve(%v) failed: %v", name, err)
	}
	if want, got := 1, len(results); want != got {
		t.Fatalf("Resolve(%v) expected %d result(s), got %d instead", name, want, got)
	}
	return results[0]
}

// The following set of functions are convenience wrappers around Update and
// Revert.

func updateExpectError(t *testing.T, name string, errID verror.ID) {
	if err := invokeUpdate(t, name); !verror.Is(err, errID) {
		t.Errorf("Unexpected update error %v, expected error ID %v", err, errID)
	}
}

func update(t *testing.T, name string) {
	if err := invokeUpdate(t, name); err != nil {
		t.Errorf("Update() failed: %v", err)
	}
}

func invokeUpdate(t *testing.T, name string) error {
	address := naming.Join(name, "nm")
	stub, err := node.BindNode(address)
	if err != nil {
		t.Fatalf("BindNode(%v) failed: %v", address, err)
	}
	return stub.Update(rt.R().NewContext())
}

func revertExpectError(t *testing.T, name string, errID verror.ID) {
	if err := invokeRevert(t, name); !verror.Is(err, errID) {
		t.Errorf("Unexpected revert error %v, expected error ID %v", err, errID)
	}
}

func revert(t *testing.T, name string) {
	if err := invokeRevert(t, name); err != nil {
		t.Errorf("Revert() failed: %v", err)
	}
}

func invokeRevert(t *testing.T, name string) error {
	address := naming.Join(name, "nm")
	stub, err := node.BindNode(address)
	if err != nil {
		t.Fatalf("BindNode(%v) failed: %v", address, err)
	}
	return stub.Revert(rt.R().NewContext())
}
