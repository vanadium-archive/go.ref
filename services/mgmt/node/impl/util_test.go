package impl_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/node"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/modules/core"
	"veyron.io/veyron/veyron/lib/testutil/security"
	"veyron.io/veyron/veyron/profiles/static"
	"veyron.io/veyron/veyron/services/mgmt/node/impl"
	"veyron.io/veyron/veyron2/services/mgmt/application"
)

// Setting this environment variable to any non-empty value avoids removing the
// node manager's workspace for successful test runs (for failed test runs, this
// is already the case).  This is useful when developing test cases.
const preserveNMWorkspaceEnv = "VEYRON_TEST_PRESERVE_NM_WORKSPACE"

func loc(d int) string {
	_, file, line, _ := runtime.Caller(d + 1)
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

func startRootMT(t *testing.T, sh *modules.Shell) (string, modules.Handle, *expect.Session) {
	h, err := sh.Start(core.RootMTCommand, "--", "--veyron.tcp.address=127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start root mount table: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	rootName := s.ExpectVar("MT_NAME")
	if t.Failed() {
		t.Fatalf("failed to read mt name: %s", s.Error())
	}
	return rootName, h, s
}

func credentialsForChild(blessing string) (string, []string) {
	creds := security.NewVeyronCredentials(rt.R().Principal(), blessing)
	return creds, []string{"VEYRON_CREDENTIALS=" + creds}
}

func createShellAndMountTable(t *testing.T) (*modules.Shell, func()) {
	sh := core.NewShell()
	// The shell, will, by default share credentials with its children.
	sh.ClearVar("VEYRON_CREDENTIALS")

	mtName, mtHandle, _ := startRootMT(t, sh)
	// Make sure the root mount table is the last process to be shutdown
	// since the others will likely want to communicate with it during
	// their shutdown process
	sh.Forget(mtHandle)

	fn := func() {
		vlog.VI(1).Info("------------ CLEANUP ------------")
		vlog.VI(1).Info("---------------------------------")
		sh.Cleanup(nil, os.Stderr)
		mtHandle.Shutdown(nil, os.Stderr)
		sh.Start(core.SetNamespaceRootsCommand, "")
	}

	if _, err := sh.Start(core.SetNamespaceRootsCommand, mtName); err != nil {
		t.Fatalf("%s: unexpected error: %s", loc(1), err)
	}
	sh.SetVar("NAMESPACE_ROOT", mtName)
	return sh, fn
}

func runShellCommand(t *testing.T, sh *modules.Shell, env []string, cmd string, args ...string) (modules.Handle, *expect.Session) {
	h, err := sh.StartWithEnv(cmd, env, args...)
	if err != nil {
		t.Fatalf("%s: failed to start %q: %s", loc(1), cmd, err)
		return nil, nil
	}
	s := expect.NewSession(t, h.Stdout(), 10*time.Second)
	s.SetVerbosity(testing.Verbose())
	return h, s
}

func envelopeFromShell(sh *modules.Shell, env []string, cmd, title string, args ...string) application.Envelope {
	args, nenv := sh.CommandEnvelope(cmd, env, args...)
	return application.Envelope{
		Title: title,
		Args:  args[1:],
		// TODO(caprita): revisit how the environment is sanitized for arbirary
		// apps.
		Env:    impl.VeyronEnvironment(nenv),
		Binary: mockBinaryRepoName,
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
	spec := static.ListenSpec
	spec.Address = "127.0.0.1:0"
	endpoint, err := server.Listen(spec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", static.ListenSpec, err)
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
		t.Fatalf("%s: BindNode(%v) failed: %v", loc(1), nodeName, err)
	}
	return stub
}

func updateNodeExpectError(t *testing.T, name string, errID verror.ID) {
	if err := nodeStub(t, name).Update(rt.R().NewContext()); !verror.Is(err, errID) {
		t.Fatalf("%s: Update(%v) expected to fail with %v, got %v instead", loc(1), name, errID, err)
	}
}

func updateNode(t *testing.T, name string) {
	if err := nodeStub(t, name).Update(rt.R().NewContext()); err != nil {
		t.Fatalf("%s: Update(%v) failed: %v", loc(1), name, err)
	}
}

func revertNodeExpectError(t *testing.T, name string, errID verror.ID) {
	if err := nodeStub(t, name).Revert(rt.R().NewContext()); !verror.Is(err, errID) {
		t.Fatalf("%s: Revert(%v) expected to fail with %v, got %v instead", loc(1), name, errID, err)
	}
}

func revertNode(t *testing.T, name string) {
	if err := nodeStub(t, name).Revert(rt.R().NewContext()); err != nil {
		t.Fatalf("%s: Revert(%v) failed: %v", loc(1), name, err)
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
		t.Fatalf("%s: BindApplication(%v) failed: %v", loc(1), appName, err)
	}
	return stub
}

func installApp(t *testing.T, opt ...veyron2.Runtime) string {
	appID, err := appStub(t).Install(ort(opt).NewContext(), mockApplicationRepoName)
	if err != nil {
		t.Fatalf("%s: Install failed: %v", loc(1), err)
	}
	return appID
}

func startAppImpl(t *testing.T, appID string, opt []veyron2.Runtime) (string, error) {
	if instanceIDs, err := appStub(t, appID).Start(ort(opt).NewContext()); err != nil {
		return "", err
	} else {
		if want, got := 1, len(instanceIDs); want != got {
			t.Fatalf("%s: Start(%v): expected %v instance ids, got %v instead", loc(1), appID, want, got)
		}
		return instanceIDs[0], nil
	}
}

func startApp(t *testing.T, appID string, opt ...veyron2.Runtime) string {
	instanceID, err := startAppImpl(t, appID, opt)
	if err != nil {
		t.Fatalf("%s: Start(%v) failed: %v", loc(1), appID, err)
	}
	return instanceID
}

func startAppExpectError(t *testing.T, appID string, expectedError verror.ID, opt ...veyron2.Runtime) {
	if _, err := startAppImpl(t, appID, opt); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf("%s: Start(%v) expected to fail with %v, got %v instead", loc(1), appID, expectedError, err)
	}
}

func stopApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(t, appID, instanceID).Stop(ort(opt).NewContext(), 5); err != nil {
		t.Fatalf("%s: Stop(%v/%v) failed: %v", loc(1), appID, instanceID, err)
	}
}

func suspendApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(t, appID, instanceID).Suspend(ort(opt).NewContext()); err != nil {
		t.Fatalf("%s: Suspend(%v/%v) failed: %v", loc(1), appID, instanceID, err)
	}
}

func resumeApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(t, appID, instanceID).Resume(ort(opt).NewContext()); err != nil {
		t.Fatalf("%s: Resume(%v/%v) failed: %v", loc(1), appID, instanceID, err)
	}
}

func resumeAppExpectError(t *testing.T, appID, instanceID string, expectedError verror.ID, opt ...veyron2.Runtime) {
	if err := appStub(t, appID, instanceID).Resume(ort(opt).NewContext()); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf("%s: Resume(%v/%v) expected to fail with %v, got %v instead", loc(1), appID, instanceID, expectedError, err)
	}
}

func updateApp(t *testing.T, appID string, opt ...veyron2.Runtime) {
	if err := appStub(t, appID).Update(ort(opt).NewContext()); err != nil {
		t.Fatalf("%s: Update(%v) failed: %v", loc(1), appID, err)
	}
}

func updateAppExpectError(t *testing.T, appID string, expectedError verror.ID) {
	if err := appStub(t, appID).Update(rt.R().NewContext()); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf("%s: Update(%v) expected to fail with %v, got %v instead", loc(1), appID, expectedError, err)
	}
}

func revertApp(t *testing.T, appID string) {
	if err := appStub(t, appID).Revert(rt.R().NewContext()); err != nil {
		t.Fatalf("%s: Revert(%v) failed: %v", loc(1), appID, err)
	}
}

func revertAppExpectError(t *testing.T, appID string, expectedError verror.ID) {
	if err := appStub(t, appID).Revert(rt.R().NewContext()); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf("%s: Revert(%v) expected to fail with %v, got %v instead", loc(1), appID, expectedError, err)
	}
}

func uninstallApp(t *testing.T, appID string) {
	if err := appStub(t, appID).Uninstall(rt.R().NewContext()); err != nil {
		t.Fatalf("%s: Uninstall(%v) failed: %v", loc(1), appID, err)
	}
}

// Code to make Association lists sortable.
type byIdentity []node.Association

func (a byIdentity) Len() int           { return len(a) }
func (a byIdentity) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byIdentity) Less(i, j int) bool { return a[i].IdentityName < a[j].IdentityName }

func compareAssociations(t *testing.T, got, expected []node.Association) {
	sort.Sort(byIdentity(got))
	sort.Sort(byIdentity(expected))
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("ListAssociations() got %v, expected %v", got, expected)
	}
}
