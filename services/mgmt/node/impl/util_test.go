package impl_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/node"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/lib/testutil/blackbox"
	"veyron.io/veyron/veyron/lib/testutil/security"
	"veyron.io/veyron/veyron/profiles"
	mtlib "veyron.io/veyron/veyron/services/mounttable/lib"
)

// Setting this environment variable to any non-empty value avoids removing the
// node manager's workspace for successful test runs (for failed test runs, this
// is already the case).  This is useful when developing test cases.
const preserveNMWorkspaceEnv = "VEYRON_TEST_PRESERVE_NM_WORKSPACE"

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
	endpoint, err := server.ListenX(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
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
// and blessings.  It returns a cleanup function.
func setupChildCommand(child *blackbox.Child) func() {
	return setupChildCommandWithBlessing(child, "child")
}

func setupChildCommandWithBlessing(child *blackbox.Child, blessing string) func() {
	cmd := child.Cmd
	for i, root := range rt.R().Namespace().Roots() {
		cmd.Env = exec.Setenv(cmd.Env, fmt.Sprintf("NAMESPACE_ROOT%d", i), root)
	}
	childcreds := security.NewVeyronCredentials(rt.R().Principal(), blessing)
	cmd.Env = exec.Setenv(cmd.Env, "VEYRON_CREDENTIALS", childcreds)
	return func() {
		os.RemoveAll(childcreds)
	}
}

// setupRootDir sets up and returns the local filesystem location that the node
// manager is told to use, as well as a cleanup function.
func setupRootDir(t *testing.T) (string, func()) {
	rootDir, err := ioutil.TempDir("", "nodemanager")
	if err != nil {
		t.Fatalf("Failed to set up temporary dir for test: %v", err)
	}
	// On some operating systems (e.g. darwin) os.TempDir() can return a
	// symlink. To avoid having to account for this eventuality later,
	// evaluate the symlink.
	rootDir, err = filepath.EvalSymlinks(rootDir)
	if err != nil {
		vlog.Fatalf("EvalSymlinks(%v) failed: %v", rootDir, err)
	}
	return rootDir, func() {
		if t.Failed() || os.Getenv(preserveNMWorkspaceEnv) != "" {
			t.Logf("You can examine the node manager workspace at %v", rootDir)
		} else {
			os.RemoveAll(rootDir)
		}
	}
}

func newServer() (ipc.Server, string) {
	server, err := rt.R().NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	endpoint, err := server.ListenX(profiles.LocalListenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
	return server, endpoint.String()
}

// resolveExpectError verifies that the given name is not in the mounttable.
func resolveExpectNotFound(t *testing.T, name string) {
	if results, err := rt.R().Namespace().Resolve(rt.R().NewContext(), name); err == nil {
		t.Fatalf("Resolve(%v) succeeded with results %v when it was expected to fail", name, results)
	} else if expectErr := verror.NoExist; !verror.Is(err, expectErr) {
		t.Fatalf("Resolve(%v) failed with error %v, expected error ID %v", name, err, expectErr)
	}
}

// resolve looks up the given name in the mounttable.
func resolve(t *testing.T, name string, replicas int) []string {
	results, err := rt.R().Namespace().Resolve(rt.R().NewContext(), name)
	if err != nil {
		t.Fatalf("Resolve(%v) failed: %v", name, err)
	}
	if want, got := replicas, len(results); want != got {
		t.Fatalf("Resolve(%v) expected %d result(s), got %d instead", name, want, got)
	}
	return results
}

// The following set of functions are convenience wrappers around Update and
// Revert for node manager.

func nodeStub(t *testing.T, name string) node.Node {
	nodeName := naming.Join(name, "nm")
	stub, err := node.BindNode(nodeName)
	if err != nil {
		t.Fatalf("BindNode(%v) failed: %v", nodeName, err)
	}
	return stub
}

func updateNodeExpectError(t *testing.T, name string, errID verror.ID) {
	if err := nodeStub(t, name).Update(rt.R().NewContext()); !verror.Is(err, errID) {
		t.Fatalf("Update(%v) expected to fail with %v, got %v instead", name, errID, err)
	}
}

func updateNode(t *testing.T, name string) {
	if err := nodeStub(t, name).Update(rt.R().NewContext()); err != nil {
		t.Fatalf("Update(%v) failed: %v", name, err)
	}
}

func revertNodeExpectError(t *testing.T, name string, errID verror.ID) {
	if err := nodeStub(t, name).Revert(rt.R().NewContext()); !verror.Is(err, errID) {
		t.Fatalf("Revert(%v) expected to fail with %v, got %v instead", name, errID, err)
	}
}

func revertNode(t *testing.T, name string) {
	if err := nodeStub(t, name).Revert(rt.R().NewContext()); err != nil {
		t.Fatalf("Revert(%v) failed: %v", name, err)
	}
}

// The following set of functions are convenience wrappers around various app
// management methods.

func ort(opt []veyron2.Runtime) veyron2.Runtime {
	if len(opt) > 0 {
		return opt[0]
	} else {
		return rt.R()
	}
}

func appStub(t *testing.T, nameComponents ...string) node.Application {
	appsName := "nm//apps"
	appName := naming.Join(append([]string{appsName}, nameComponents...)...)
	stub, err := node.BindApplication(appName)
	if err != nil {
		t.Fatalf("BindApplication(%v) failed: %v", appName, err)
	}
	return stub
}

func installApp(t *testing.T, opt ...veyron2.Runtime) string {
	appID, err := appStub(t).Install(ort(opt).NewContext(), "ar")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	return appID
}

func startAppImpl(t *testing.T, appID string, opt []veyron2.Runtime) (string, error) {
	if instanceIDs, err := appStub(t, appID).Start(ort(opt).NewContext()); err != nil {
		return "", err
	} else {
		if want, got := 1, len(instanceIDs); want != got {
			t.Fatalf("Start(%v): expected %v instance ids, got %v instead", appID, want, got)
		}
		return instanceIDs[0], nil
	}
}

func startApp(t *testing.T, appID string, opt ...veyron2.Runtime) string {
	instanceID, err := startAppImpl(t, appID, opt)
	if err != nil {
		t.Fatalf("Start(%v) failed: %v", appID, err)
	}
	return instanceID
}

func startAppExpectError(t *testing.T, appID string, expectedError verror.ID, opt ...veyron2.Runtime) {
	if _, err := startAppImpl(t, appID, opt); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf("Start(%v) expected to fail with %v, got %v instead", appID, expectedError, err)
	}
}

func stopApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(t, appID, instanceID).Stop(ort(opt).NewContext(), 5); err != nil {
		t.Fatalf("Stop(%v/%v) failed: %v", appID, instanceID, err)
	}
}

func suspendApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(t, appID, instanceID).Suspend(ort(opt).NewContext()); err != nil {
		t.Fatalf("Suspend(%v/%v) failed: %v", appID, instanceID, err)
	}
}

func resumeApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(t, appID, instanceID).Resume(ort(opt).NewContext()); err != nil {
		t.Fatalf("Resume(%v/%v) failed: %v", appID, instanceID, err)
	}
}

func updateApp(t *testing.T, appID string, opt ...veyron2.Runtime) {
	if err := appStub(t, appID).Update(ort(opt).NewContext()); err != nil {
		t.Fatalf("Update(%v) failed: %v", appID, err)
	}
}

func updateAppExpectError(t *testing.T, appID string, expectedError verror.ID) {
	if err := appStub(t, appID).Update(rt.R().NewContext()); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf("Update(%v) expected to fail with %v, got %v instead", appID, expectedError, err)
	}
}

func revertApp(t *testing.T, appID string) {
	if err := appStub(t, appID).Revert(rt.R().NewContext()); err != nil {
		t.Fatalf("Revert(%v) failed: %v", appID, err)
	}
}

func revertAppExpectError(t *testing.T, appID string, expectedError verror.ID) {
	if err := appStub(t, appID).Revert(rt.R().NewContext()); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf("Revert(%v) expected to fail with %v, got %v instead", appID, expectedError, err)
	}
}

func uninstallApp(t *testing.T, appID string) {
	if err := appStub(t, appID).Uninstall(rt.R().NewContext()); err != nil {
		t.Fatalf("Uninstall(%v) failed: %v", appID, err)
	}
}
