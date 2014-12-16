package impl_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/device"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/flags/consts"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/modules/core"
	tsecurity "veyron.io/veyron/veyron/lib/testutil/security"
	_ "veyron.io/veyron/veyron/profiles/static"
	"veyron.io/veyron/veyron/services/mgmt/device/impl"
	"veyron.io/veyron/veyron2/services/mgmt/application"
)

const (
	// Setting this environment variable to any non-empty value avoids
	// removing the device manager's workspace for successful test runs (for
	// failed test runs, this is already the case).  This is useful when
	// developing test cases.
	preserveDMWorkspaceEnv = "VEYRON_TEST_PRESERVE_DM_WORKSPACE"

	// TODO(caprita): Set the timeout in a more principled manner.
	expectTimeout = 20 * time.Second
)

func loc(d int) string {
	_, file, line, _ := runtime.Caller(d + 1)
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

func startRootMT(t *testing.T, sh *modules.Shell) (string, modules.Handle) {
	h, err := sh.Start(core.RootMTCommand, nil, "--", "--veyron.tcp.address=127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start root mount table: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), expectTimeout)
	s.ExpectVar("PID")
	rootName := s.ExpectVar("MT_NAME")
	if t.Failed() {
		t.Fatalf("failed to read mt name: %s", s.Error())
	}
	return rootName, h
}

func credentialsForChild(blessing string) (string, []string) {
	creds, _ := tsecurity.ForkCredentials(globalRT.Principal(), blessing)
	return creds, []string{consts.VeyronCredentials + "=" + creds}
}

// setNSRoots sets the roots for the local runtime's namespace.
func setNSRoots(t *testing.T, roots ...string) {
	if err := globalRT.Namespace().SetRoots(roots...); err != nil {
		t.Fatalf("%s: SetRoots(%v) failed with %v", loc(2), roots, err)
	}
}

func createShellAndMountTable(t *testing.T) (*modules.Shell, func()) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// The shell, will, by default share credentials with its children.
	sh.ClearVar(consts.VeyronCredentials)

	mtName, mtHandle := startRootMT(t, sh)
	vlog.VI(1).Infof("Started shell mounttable with name %v", mtName)
	// Make sure the root mount table is the last process to be shutdown
	// since the others will likely want to communicate with it during
	// their shutdown process
	sh.Forget(mtHandle)

	// TODO(caprita): Define a GetNamespaceRootsCommand in modules/core and
	// use that?
	oldNamespaceRoots := globalRT.Namespace().Roots()
	fn := func() {
		vlog.VI(1).Info("------------ CLEANUP ------------")
		vlog.VI(1).Info("---------------------------------")
		vlog.VI(1).Info("--(cleaning up shell)------------")
		if err := sh.Cleanup(os.Stdout, os.Stderr); err != nil {
			t.Fatalf("sh.Cleanup failed with %v", err)
		}
		vlog.VI(1).Info("--(done cleaning up shell)-------")
		vlog.VI(1).Info("--(shutting down root mt)--------")
		if err := mtHandle.Shutdown(os.Stdout, os.Stderr); err != nil {
			t.Fatalf("mtHandle.Shutdown failed with %v", err)
		}
		vlog.VI(1).Info("--(done shutting down root mt)---")
		vlog.VI(1).Info("--------- DONE CLEANUP ----------")
		setNSRoots(t, oldNamespaceRoots...)
	}
	setNSRoots(t, mtName)
	sh.SetVar(consts.NamespaceRootPrefix, mtName)
	return sh, fn
}

func runShellCommand(t *testing.T, sh *modules.Shell, env []string, cmd string, args ...string) (modules.Handle, *expect.Session) {
	h, err := sh.Start(cmd, env, args...)
	if err != nil {
		t.Fatalf("%s: failed to start %q: %s", loc(1), cmd, err)
		return nil, nil
	}
	s := expect.NewSession(t, h.Stdout(), expectTimeout)
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

// setupRootDir sets up and returns the local filesystem location that the
// device manager is told to use, as well as a cleanup function.
func setupRootDir(t *testing.T) (string, func()) {
	rootDir, err := ioutil.TempDir("", "devicemanager")
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
		if t.Failed() || os.Getenv(preserveDMWorkspaceEnv) != "" {
			t.Logf("You can examine the device manager workspace at %v", rootDir)
		} else {
			os.RemoveAll(rootDir)
		}
	}
}

func newServer() (ipc.Server, string) {
	server, err := globalRT.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	spec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	endpoints, err := server.Listen(spec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", spec, err)
	}
	return server, endpoints[0].String()
}

// resolveExpectNotFound verifies that the given name is not in the mounttable.
func resolveExpectNotFound(t *testing.T, name string) {
	if results, err := globalRT.Namespace().Resolve(globalRT.NewContext(), name); err == nil {
		t.Fatalf("%s: Resolve(%v) succeeded with results %v when it was expected to fail", loc(1), name, results)
	} else if expectErr := naming.ErrNoSuchName.ID; !verror2.Is(err, expectErr) {
		t.Fatalf("%s: Resolve(%v) failed with error %v, expected error ID %v", loc(1), name, err, expectErr)
	}
}

// resolve looks up the given name in the mounttable.
func resolve(t *testing.T, name string, replicas int) []string {
	results, err := globalRT.Namespace().Resolve(globalRT.NewContext(), name)
	if err != nil {
		t.Fatalf("Resolve(%v) failed: %v", name, err)
	}

	filteredResults := []string{}
	for _, r := range results {
		if strings.Index(r, "@tcp") != -1 {
			filteredResults = append(filteredResults, r)
		}
	}
	// We are going to get a websocket and a tcp endpoint for each replica.
	if want, got := replicas, len(filteredResults); want != got {
		t.Fatalf("Resolve(%v) expected %d result(s), got %d instead", name, want, got)
	}
	return filteredResults
}

// The following set of functions are convenience wrappers around Update and
// Revert for device manager.

func deviceStub(name string) device.DeviceClientMethods {
	deviceName := naming.Join(name, "device")
	return device.DeviceClient(deviceName)
}

func updateDeviceExpectError(t *testing.T, name string, errID verror.ID) {
	if err := deviceStub(name).Update(globalRT.NewContext()); !verror2.Is(err, errID) {
		t.Fatalf("%s: Update(%v) expected to fail with %v, got %v instead", loc(1), name, errID, err)
	}
}

func updateDevice(t *testing.T, name string) {
	if err := deviceStub(name).Update(globalRT.NewContext()); err != nil {
		t.Fatalf("%s: Update(%v) failed: %v", loc(1), name, err)
	}
}

func revertDeviceExpectError(t *testing.T, name string, errID verror.ID) {
	if err := deviceStub(name).Revert(globalRT.NewContext()); !verror2.Is(err, errID) {
		t.Fatalf("%s: Revert(%v) expected to fail with %v, got %v instead", loc(1), name, errID, err)
	}
}

func revertDevice(t *testing.T, name string) {
	if err := deviceStub(name).Revert(globalRT.NewContext()); err != nil {
		t.Fatalf("%s: Revert(%v) failed: %v", loc(1), name, err)
	}
}

// The following set of functions are convenience wrappers around various app
// management methods.

func ort(opt []veyron2.Runtime) veyron2.Runtime {
	if len(opt) > 0 {
		return opt[0]
	} else {
		return globalRT
	}
}

func appStub(nameComponents ...string) device.ApplicationClientMethods {
	appsName := "dm//apps"
	appName := naming.Join(append([]string{appsName}, nameComponents...)...)
	return device.ApplicationClient(appName)
}

func installApp(t *testing.T, opt ...veyron2.Runtime) string {
	appID, err := appStub().Install(ort(opt).NewContext(), mockApplicationRepoName)
	if err != nil {
		t.Fatalf("%s: Install failed: %v", loc(1), err)
	}
	return appID
}

type granter struct {
	ipc.CallOpt
	p         security.Principal
	extension string
}

func (g *granter) Grant(other security.Blessings) (security.Blessings, error) {
	return g.p.Bless(other.PublicKey(), g.p.BlessingStore().Default(), g.extension, security.UnconstrainedUse())
}

func startAppImpl(t *testing.T, appID, grant string, opt ...veyron2.Runtime) (string, error) {
	var opts []ipc.CallOpt
	if grant != "" {
		opts = append(opts, &granter{p: ort(opt).Principal(), extension: grant})
	}
	if instanceIDs, err := appStub(appID).Start(ort(opt).NewContext(), opts...); err != nil {
		return "", err
	} else {
		if want, got := 1, len(instanceIDs); want != got {
			t.Fatalf("%s: Start(%v): expected %v instance ids, got %v instead", loc(1), appID, want, got)
		}
		return instanceIDs[0], nil
	}
}

func startApp(t *testing.T, appID string, opt ...veyron2.Runtime) string {
	instanceID, err := startAppImpl(t, appID, "forapp", opt...)
	if err != nil {
		t.Fatalf("%s: Start(%v) failed: %v", loc(1), appID, err)
	}
	return instanceID
}

func startAppExpectError(t *testing.T, appID string, expectedError verror.ID, opt ...veyron2.Runtime) {
	if _, err := startAppImpl(t, appID, "forapp", opt...); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf("%s: Start(%v) expected to fail with %v, got %v instead", loc(1), appID, expectedError, err)
	}
}

func stopApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(appID, instanceID).Stop(ort(opt).NewContext(), 5); err != nil {
		t.Fatalf("%s: Stop(%v/%v) failed: %v", loc(1), appID, instanceID, err)
	}
}

func suspendApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(appID, instanceID).Suspend(ort(opt).NewContext()); err != nil {
		t.Fatalf("%s: Suspend(%v/%v) failed: %v", loc(1), appID, instanceID, err)
	}
}

func resumeApp(t *testing.T, appID, instanceID string, opt ...veyron2.Runtime) {
	if err := appStub(appID, instanceID).Resume(ort(opt).NewContext()); err != nil {
		t.Fatalf("%s: Resume(%v/%v) failed: %v", loc(1), appID, instanceID, err)
	}
}

func resumeAppExpectError(t *testing.T, appID, instanceID string, expectedError verror.ID, opt ...veyron2.Runtime) {
	if err := appStub(appID, instanceID).Resume(ort(opt).NewContext()); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf("%s: Resume(%v/%v) expected to fail with %v, got %v instead", loc(1), appID, instanceID, expectedError, err)
	}
}

func updateApp(t *testing.T, appID string, opt ...veyron2.Runtime) {
	if err := appStub(appID).Update(ort(opt).NewContext()); err != nil {
		t.Fatalf("%s: Update(%v) failed: %v", loc(1), appID, err)
	}
}

func updateAppExpectError(t *testing.T, appID string, expectedError verror.ID) {
	if err := appStub(appID).Update(globalRT.NewContext()); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf("%s: Update(%v) expected to fail with %v, got %v instead", loc(1), appID, expectedError, err)
	}
}

func revertApp(t *testing.T, appID string) {
	if err := appStub(appID).Revert(globalRT.NewContext()); err != nil {
		t.Fatalf("%s: Revert(%v) failed: %v", loc(1), appID, err)
	}
}

func revertAppExpectError(t *testing.T, appID string, expectedError verror.ID) {
	if err := appStub(appID).Revert(globalRT.NewContext()); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf("%s: Revert(%v) expected to fail with %v, got %v instead", loc(1), appID, expectedError, err)
	}
}

func uninstallApp(t *testing.T, appID string) {
	if err := appStub(appID).Uninstall(globalRT.NewContext()); err != nil {
		t.Fatalf("%s: Uninstall(%v) failed: %v", loc(1), appID, err)
	}
}

// Code to make Association lists sortable.
type byIdentity []device.Association

func (a byIdentity) Len() int           { return len(a) }
func (a byIdentity) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byIdentity) Less(i, j int) bool { return a[i].IdentityName < a[j].IdentityName }

func compareAssociations(t *testing.T, got, expected []device.Association) {
	sort.Sort(byIdentity(got))
	sort.Sort(byIdentity(expected))
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("ListAssociations() got %v, expected %v", got, expected)
	}
}

// generateSuidHelperScript builds a script to execute the test target as
// a suidhelper instance and returns the path to the script.
func generateSuidHelperScript(t *testing.T, root string) string {
	output := "#!/bin/bash\n"
	output += "VEYRON_SUIDHELPER_TEST=1"
	output += " "
	output += "exec " + os.Args[0] + " -minuid=1 -test.run=TestSuidHelper $*"
	output += "\n"

	vlog.VI(1).Infof("script\n%s", output)

	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	// Helper does not need to live under the device manager's root dir, but
	// we put it there for convenience.
	path := filepath.Join(root, "helper.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		t.Fatalf("WriteFile(%v) failed: %v", path, err)
	}
	return path
}
