// TODO(caprita): This file is becoming unmanageable; split into several test
// files.
// TODO(rjkroege): Add a more extensive unit test case to exercise ACL logic.

package impl_test

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	goexec "os/exec"
	"os/user"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/device"
	"v.io/core/veyron2/services/mgmt/logreader"
	"v.io/core/veyron2/services/mgmt/pprof"
	"v.io/core/veyron2/services/mgmt/stats"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/vdl/vdlutil"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	binaryimpl "v.io/core/veyron/services/mgmt/binary/impl"
	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/device/impl"
	libbinary "v.io/core/veyron/services/mgmt/lib/binary"
	mgmttest "v.io/core/veyron/services/mgmt/lib/testutil"
	suidhelper "v.io/core/veyron/services/mgmt/suidhelper/impl"
)

const (
	execScriptCmd    = "execScriptCmd"
	deviceManagerCmd = "deviceManager"
	appCmd           = "app"
	installerCmd     = "installer"
	uninstallerCmd   = "uninstaller"
)

func init() {
	// TODO(rthellend): Remove when vom2 is ready.
	vdlutil.Register(&naming.VDLMountedServer{})

	modules.RegisterChild(execScriptCmd, "", execScript)
	modules.RegisterChild(deviceManagerCmd, "", deviceManager)
	modules.RegisterChild(appCmd, "", app)
	modules.RegisterChild(installerCmd, "", install)
	modules.RegisterChild(uninstallerCmd, "", uninstall)
	testutil.Init()

	if modules.IsModulesProcess() {
		return
	}
	initRT(options.RuntimePrincipal{tsecurity.NewPrincipal("test-principal")})
}

var globalRT veyron2.Runtime

func initRT(opts ...veyron2.ROpt) {
	var err error
	if globalRT, err = rt.New(opts...); err != nil {
		panic(err)
	}

	// Disable the cache because we will be manipulating/using the namespace
	// across multiple processes and want predictable behaviour without
	// relying on timeouts.
	globalRT.Namespace().CacheCtl(naming.DisableCache(true))
}

// TestHelperProcess is the entrypoint for the modules commands in a
// a test subprocess.
func TestHelperProcess(t *testing.T) {
	initRT()
	modules.DispatchInTest()
}

// TestSuidHelper is testing boilerplate for suidhelper that does not
// create a runtime because the suidhelper is not a Veyron application.
func TestSuidHelper(t *testing.T) {
	if os.Getenv("VEYRON_SUIDHELPER_TEST") != "1" {
		return
	}
	vlog.VI(1).Infof("TestSuidHelper starting")

	if err := suidhelper.Run(os.Environ()); err != nil {
		vlog.Fatalf("Failed to Run() setuidhelper: %v", err)
	}
}

// execScript launches the script passed as argument.
func execScript(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	args = args[1:]
	if want, got := 1, len(args); want != got {
		vlog.Fatalf("execScript expected %d arguments, got %d instead", want, got)
	}
	script := args[0]
	osenv := []string{}
	if env["PAUSE_BEFORE_STOP"] == "1" {
		osenv = append(osenv, "PAUSE_BEFORE_STOP=1")
	}

	cmd := goexec.Cmd{
		Path:   script,
		Env:    osenv,
		Stdin:  stdin,
		Stderr: stderr,
		Stdout: stdout,
	}

	return cmd.Run()
}

// deviceManager sets up a device manager server.  It accepts the name to
// publish the server under as an argument.  Additional arguments can optionally
// specify device manager config settings.
func deviceManager(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	args = args[1:]
	if len(args) == 0 {
		vlog.Fatalf("deviceManager expected at least an argument")
	}
	publishName := args[0]
	args = args[1:]
	defer fmt.Fprintf(stdout, "%v terminating\n", publishName)
	defer vlog.VI(1).Infof("%v terminating", publishName)
	defer globalRT.Cleanup()
	server, endpoint := mgmttest.NewServer(globalRT)
	defer server.Stop()
	name := naming.JoinAddressName(endpoint, "")
	vlog.VI(1).Infof("Device manager name: %v", name)

	// Satisfy the contract described in doc.go by passing the config state
	// through to the device manager dispatcher constructor.
	configState, err := config.Load()
	if err != nil {
		vlog.Fatalf("Failed to decode config state: %v", err)
	}
	configState.Name = name

	// This exemplifies how to override or set specific config fields, if,
	// for example, the device manager is invoked 'by hand' instead of via a
	// script prepared by a previous version of the device manager.
	if len(args) > 0 {
		if want, got := 4, len(args); want != got {
			vlog.Fatalf("expected %d additional arguments, got %d instead", want, got)
		}
		configState.Root, configState.Helper, configState.Origin, configState.CurrentLink = args[0], args[1], args[2], args[3]
	}
	dispatcher, err := impl.NewDispatcher(globalRT.Principal(), configState, func() { fmt.Println("stop handler") })
	if err != nil {
		vlog.Fatalf("Failed to create device manager dispatcher: %v", err)
	}
	if err := server.ServeDispatcher(publishName, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}
	impl.InvokeCallback(globalRT.NewContext(), name)

	fmt.Fprintf(stdout, "ready:%d\n", os.Getpid())

	<-signals.ShutdownOnSignals(globalRT)

	if val, present := env["PAUSE_BEFORE_STOP"]; present && val == "1" {
		modules.WaitForEOF(stdin)
	}
	if dispatcher.Leaking() {
		vlog.Fatalf("device manager leaking resources")
	}
	return nil
}

// install installs the device manager.
func install(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	args = args[1:]
	// args[0] is the entrypoint for the binary to be run from the shell
	// script that SelfInstall will write out.
	entrypoint := args[0]
	// Overwrite the entrypoint in our environment (i.e. the one that got us
	// here), with the one we want written out in the shell script.
	osenv := modules.SetEntryPoint(env, entrypoint)
	if args[1] != "--" {
		vlog.Fatalf("expected '--' immediately following command name")
	}
	args = append([]string{""}, args[2:]...) // skip the cmd and '--'
	if err := impl.SelfInstall(args, osenv); err != nil {
		vlog.Fatalf("SelfInstall failed: %v", err)
		return err
	}
	return nil
}

// uninstall uninstalls the device manager.
func uninstall(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if err := impl.Uninstall(); err != nil {
		vlog.Fatalf("Uninstall failed: %v", err)
		return err
	}
	return nil
}

// appService defines a test service that the test app should be running.
// TODO(caprita): Use this to make calls to the app and verify how Suspend/Stop
// interact with an active service.
type appService struct{}

func (appService) Echo(_ ipc.ServerContext, message string) (string, error) {
	return message, nil
}

func (appService) Cat(_ ipc.ServerContext, file string) (string, error) {
	if file == "" || file[0] == filepath.Separator || file[0] == '.' {
		return "", fmt.Errorf("illegal file name: %q", file)
	}
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func ping() {
	if call, err := globalRT.Client().StartCall(globalRT.NewContext(), "pingserver", "Ping", []interface{}{os.Getenv(suidhelper.SavedArgs)}); err != nil {
		vlog.Fatalf("StartCall failed: %v", err)
	} else if err := call.Finish(); err != nil {
		vlog.Fatalf("Finish failed: %v", err)
	}
}

func cat(name, file string) (string, error) {
	ctx, cancel := context.WithTimeout(globalRT.NewContext(), time.Minute)
	defer cancel()
	call, err := globalRT.Client().StartCall(ctx, name, "Cat", []interface{}{file})
	if err != nil {
		return "", err
	}
	var content string
	if ferr := call.Finish(&content, &err); ferr != nil {
		err = ferr
	}
	return content, err
}

func app(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	args = args[1:]
	if expected, got := 1, len(args); expected != got {
		vlog.Fatalf("Unexpected number of arguments: expected %d, got %d", expected, got)
	}
	publishName := args[0]

	defer globalRT.Cleanup()
	server, _ := mgmttest.NewServer(globalRT)
	defer server.Stop()
	if err := server.Serve(publishName, new(appService), nil); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}
	// Some of our tests look for log files, so make sure they are flushed
	// to ensure that at least the files exist.
	vlog.FlushLog()
	ping()

	<-signals.ShutdownOnSignals(globalRT)
	if err := ioutil.WriteFile("testfile", []byte("goodbye world"), 0600); err != nil {
		vlog.Fatalf("Failed to write testfile: %v", err)
	}
	return nil
}

// TODO(rjkroege): generateDeviceManagerScript and generateSuidHelperScript have
// code similarity that might benefit from refactoring.
// generateDeviceManagerScript is very similar in behavior to generateScript in
// device_invoker.go.  However, we chose to re-implement it here for two
// reasons: (1) avoid making generateScript public; and (2) how the test choses
// to invoke the device manager subprocess the first time should be independent
// of how device manager implementation sets up its updated versions.
func generateDeviceManagerScript(t *testing.T, root string, args, env []string) string {
	env = impl.VeyronEnvironment(env)
	output := "#!/bin/bash\n"
	output += strings.Join(config.QuoteEnv(env), " ") + " exec "
	output += strings.Join(args, " ")
	if err := os.MkdirAll(filepath.Join(root, "factory"), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	// Why pigeons? To show that the name we choose for the initial script
	// doesn't matter and in particular is independent of how device manager
	// names its updated version scripts (deviced.sh).
	path := filepath.Join(root, "factory", "pigeons.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		t.Fatalf("WriteFile(%v) failed: %v", path, err)
	}
	return path
}

// TestDeviceManagerUpdateAndRevert makes the device manager go through the
// motions of updating itself to newer versions (twice), and reverting itself
// back (twice). It also checks that update and revert fail when they're
// supposed to. The initial device manager is started 'by hand' via a module
// command. Further versions are started through the soft link that the device
// manager itself updates.
func TestDeviceManagerUpdateAndRevert(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Current link does not have to live in the root dir, but it's
	// convenient to put it there so we have everything in one place.
	currLink := filepath.Join(root, "current_link")

	crDir, crEnv := mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)
	dmArgs := []string{"factoryDM", root, "unused_helper", mockApplicationRepoName, currLink}
	args, env := sh.CommandEnvelope(deviceManagerCmd, crEnv, dmArgs...)

	scriptPathFactory := generateDeviceManagerScript(t, root, args, env)

	if err := os.Symlink(scriptPathFactory, currLink); err != nil {
		t.Fatalf("Symlink(%q, %q) failed: %v", scriptPathFactory, currLink, err)
	}

	// We instruct the initial device manager that we run to pause before
	// stopping its service, so that we get a chance to verify that
	// attempting an update while another one is ongoing will fail.
	dmPauseBeforeStopEnv := append(crEnv, "PAUSE_BEFORE_STOP=1")

	// Start the initial version of the device manager, the so-called
	// "factory" version. We use the modules-generated command to start it.
	// We could have also used the scriptPathFactory to start it, but this
	// demonstrates that the initial device manager could be started by hand
	// as long as the right initial configuration is passed into the device
	// manager implementation.
	dmh, dms := mgmttest.RunShellCommand(t, sh, dmPauseBeforeStopEnv, deviceManagerCmd, dmArgs...)
	defer func() {
		syscall.Kill(dmh.Pid(), syscall.SIGINT)
	}()

	mgmttest.ReadPID(t, dms)
	resolve(t, "factoryDM", 1) // Verify the device manager has published itself.

	// Simulate an invalid envelope in the application repository.
	*envelope = envelopeFromShell(sh, dmPauseBeforeStopEnv, deviceManagerCmd, "bogus", dmArgs...)

	updateDeviceExpectError(t, "factoryDM", impl.ErrAppTitleMismatch.ID)
	revertDeviceExpectError(t, "factoryDM", impl.ErrUpdateNoOp.ID)

	// Set up a second version of the device manager. The information in the
	// envelope will be used by the device manager to stage the next
	// version.
	crDir, crEnv = mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)
	*envelope = envelopeFromShell(sh, crEnv, deviceManagerCmd, application.DeviceManagerTitle, "v2DM")
	updateDevice(t, "factoryDM")

	// Current link should have been updated to point to v2.
	evalLink := func() string {
		path, err := filepath.EvalSymlinks(currLink)
		if err != nil {
			t.Fatalf("EvalSymlinks(%v) failed: %v", currLink, err)
		}
		return path
	}
	scriptPathV2 := evalLink()
	if scriptPathFactory == scriptPathV2 {
		t.Fatalf("current link didn't change")
	}

	updateDeviceExpectError(t, "factoryDM", impl.ErrOperationInProgress.ID)

	dmh.CloseStdin()

	dms.Expect("factoryDM terminating")
	dmh.Shutdown(os.Stderr, os.Stderr)

	// A successful update means the device manager has stopped itself.  We
	// relaunch it from the current link.
	resolveExpectNotFound(t, "v2DM") // Ensure a clean slate.

	dmh, dms = mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)

	mgmttest.ReadPID(t, dms)
	resolve(t, "v2DM", 1) // Current link should have been launching v2.

	// Try issuing an update without changing the envelope in the
	// application repository: this should fail, and current link should be
	// unchanged.
	updateDeviceExpectError(t, "v2DM", impl.ErrUpdateNoOp.ID)
	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Create a third version of the device manager and issue an update.
	crDir, crEnv = mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)
	*envelope = envelopeFromShell(sh, crEnv, deviceManagerCmd, application.DeviceManagerTitle, "v3DM")
	updateDevice(t, "v2DM")

	scriptPathV3 := evalLink()
	if scriptPathV3 == scriptPathV2 {
		t.Fatalf("current link didn't change")
	}

	dms.Expect("v2DM terminating")

	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, "v3DM") // Ensure a clean slate.

	// Re-lanuch the device manager from current link.  We instruct the
	// device manager to pause before stopping its server, so that we can
	// verify that a second revert fails while a revert is in progress.
	dmh, dms = mgmttest.RunShellCommand(t, sh, dmPauseBeforeStopEnv, execScriptCmd, currLink)

	mgmttest.ReadPID(t, dms)
	resolve(t, "v3DM", 1) // Current link should have been launching v3.

	// Revert the device manager to its previous version (v2).
	revertDevice(t, "v3DM")
	revertDeviceExpectError(t, "v3DM", impl.ErrOperationInProgress.ID) // Revert already in progress.
	dmh.CloseStdin()
	dms.Expect("v3DM terminating")
	if evalLink() != scriptPathV2 {
		t.Fatalf("current link was not reverted correctly")
	}
	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, "v2DM") // Ensure a clean slate.

	dmh, dms = mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dms)
	resolve(t, "v2DM", 1) // Current link should have been launching v2.

	// Revert the device manager to its previous version (factory).
	revertDevice(t, "v2DM")
	dms.Expect("v2DM terminating")
	if evalLink() != scriptPathFactory {
		t.Fatalf("current link was not reverted correctly")
	}
	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, "factoryDM") // Ensure a clean slate.

	dmh, dms = mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dms)
	resolve(t, "factoryDM", 1) // Current link should have been launching factory version.
	stopDevice(t, "factoryDM")
	dms.Expect("stop handler")
	dms.Expect("factoryDM terminating")
	dms.ExpectEOF()

	// Re-launch the device manager, to exercise the behavior of Suspend.
	resolveExpectNotFound(t, "factoryDM") // Ensure a clean slate.
	dmh, dms = mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dms)
	resolve(t, "factoryDM", 1)
	suspendDevice(t, "factoryDM")
	dms.Expect("factoryDM terminating")
	dms.ExpectEOF()
}

type pingServer chan<- string

// TODO(caprita): Set the timeout in a more principled manner.
const pingTimeout = 60 * time.Second

func (p pingServer) Ping(_ ipc.ServerContext, arg string) {
	p <- arg
}

// setupPingServer creates a server listening for a ping from a child app; it
// returns a channel on which the app's ping message is returned, and a cleanup
// function.
func setupPingServer(t *testing.T) (<-chan string, func()) {
	server, _ := mgmttest.NewServer(globalRT)
	pingCh := make(chan string, 1)
	if err := server.Serve("pingserver", pingServer(pingCh), &openAuthorizer{}); err != nil {
		t.Fatalf("Serve(%q, <dispatcher>) failed: %v", "pingserver", err)
	}
	return pingCh, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func verifyAppWorkspace(t *testing.T, root, appID, instanceID string) {
	// HACK ALERT: for now, we peek inside the device manager's directory
	// structure (which ought to be opaque) to check for what the app has
	// written to its local root.
	//
	// TODO(caprita): add support to device manager to browse logs/app local
	// root.
	applicationDirName := func(title string) string {
		h := md5.New()
		h.Write([]byte(title))
		hash := strings.TrimRight(base64.URLEncoding.EncodeToString(h.Sum(nil)), "=")
		return "app-" + hash
	}
	components := strings.Split(appID, "/")
	appTitle, installationID := components[0], components[1]
	instanceDir := filepath.Join(root, applicationDirName(appTitle), "installation-"+installationID, "instances", "instance-"+instanceID)
	rootDir := filepath.Join(instanceDir, "root")
	testFile := filepath.Join(rootDir, "testfile")
	if read, err := ioutil.ReadFile(testFile); err != nil {
		t.Fatalf("Failed to read %v: %v", testFile, err)
	} else if want, got := "goodbye world", string(read); want != got {
		t.Fatalf("Expected to read %v, got %v instead", want, got)
	}
	// END HACK
}

// TODO(rjkroege): Consider validating additional parameters.
func verifyHelperArgs(t *testing.T, pingCh <-chan string, username string) {
	var env string
	select {
	case env = <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf(testutil.FormatLogLine(2, "%s: failed to get ping"))
	}
	d := json.NewDecoder(strings.NewReader(env))
	var savedArgs suidhelper.ArgsSavedForTest

	if err := d.Decode(&savedArgs); err != nil {
		t.Fatalf("failed to decode preserved argument %v: %v", env, err)
	}

	if savedArgs.Uname != username {
		t.Fatalf("got username %v, expected username %v", savedArgs.Uname, username)
	}
}

// TestAppLifeCycle installs an app, starts it, suspends it, resumes it, and
// then stops it.
func TestAppLifeCycle(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	crDir, crEnv := mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dms)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	resolve(t, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := installApp(t)

	// Start requires the caller to grant a blessing for the app instance.
	if _, err := startAppImpl(t, appID, ""); err == nil || !verror.Is(err, impl.ErrInvalidBlessing.ID) {
		t.Fatalf("Start(%v) expected to fail with %v, got %v instead", appID, impl.ErrInvalidBlessing.ID, err)
	}

	// Start an instance of the app.
	instance1ID := startApp(t, appID)

	// Wait until the app pings us that it's ready.
	verifyHelperArgs(t, pingCh, userName(t))

	v1EP1 := resolve(t, "appV1", 1)[0]

	// Suspend the app instance.
	suspendApp(t, appID, instance1ID)
	resolveExpectNotFound(t, "appV1")

	resumeApp(t, appID, instance1ID)
	verifyHelperArgs(t, pingCh, userName(t)) // Wait until the app pings us that it's ready.
	oldV1EP1 := v1EP1
	if v1EP1 = resolve(t, "appV1", 1)[0]; v1EP1 == oldV1EP1 {
		t.Fatalf("Expected a new endpoint for the app after suspend/resume")
	}

	// Start a second instance.
	instance2ID := startApp(t, appID)
	verifyHelperArgs(t, pingCh, userName(t)) // Wait until the app pings us that it's ready.

	// There should be two endpoints mounted as "appV1", one for each
	// instance of the app.
	endpoints := resolve(t, "appV1", 2)
	v1EP2 := endpoints[0]
	if endpoints[0] == v1EP1 {
		v1EP2 = endpoints[1]
		if v1EP2 == v1EP1 {
			t.Fatalf("Both endpoints are the same")
		}
	} else if endpoints[1] != v1EP1 {
		t.Fatalf("Second endpoint should have been v1EP1: %v, %v", endpoints, v1EP1)
	}

	// TODO(caprita): test Suspend and Resume, and verify various
	// non-standard combinations (suspend when stopped; resume while still
	// running; stop while suspended).

	// Suspend the first instance.
	suspendApp(t, appID, instance1ID)
	// Only the second instance should still be running and mounted.
	if want, got := v1EP2, resolve(t, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Updating the installation to itself is a no-op.
	updateAppExpectError(t, appID, impl.ErrUpdateNoOp.ID)

	// Updating the installation should not work with a mismatched title.
	*envelope = envelopeFromShell(sh, nil, appCmd, "bogus")

	updateAppExpectError(t, appID, impl.ErrAppTitleMismatch.ID)

	// Create a second version of the app and update the app to it.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV2")

	updateApp(t, appID)

	// Second instance should still be running.
	if want, got := v1EP2, resolve(t, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Resume first instance.
	resumeApp(t, appID, instance1ID)
	verifyHelperArgs(t, pingCh, userName(t)) // Wait until the app pings us that it's ready.
	// Both instances should still be running the first version of the app.
	// Check that the mounttable contains two endpoints, one of which is
	// v1EP2.
	endpoints = resolve(t, "appV1", 2)
	if endpoints[0] == v1EP2 {
		if endpoints[1] == v1EP2 {
			t.Fatalf("Both endpoints are the same")
		}
	} else if endpoints[1] != v1EP2 {
		t.Fatalf("Second endpoint should have been v1EP2: %v, %v", endpoints, v1EP2)
	}

	// Stop first instance.
	stopApp(t, appID, instance1ID)
	verifyAppWorkspace(t, root, appID, instance1ID)

	// Only second instance is still running.
	if want, got := v1EP2, resolve(t, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Start a third instance.
	instance3ID := startApp(t, appID)
	// Wait until the app pings us that it's ready.
	verifyHelperArgs(t, pingCh, userName(t))

	resolve(t, "appV2", 1)

	// Stop second instance.
	stopApp(t, appID, instance2ID)
	resolveExpectNotFound(t, "appV1")

	// Stop third instance.
	stopApp(t, appID, instance3ID)
	resolveExpectNotFound(t, "appV2")

	// Revert the app.
	revertApp(t, appID)

	// Start a fourth instance.  It should be started from version 1.
	instance4ID := startApp(t, appID)
	verifyHelperArgs(t, pingCh, userName(t)) // Wait until the app pings us that it's ready.
	resolve(t, "appV1", 1)
	stopApp(t, appID, instance4ID)
	resolveExpectNotFound(t, "appV1")

	// We are already on the first version, no further revert possible.
	revertAppExpectError(t, appID, impl.ErrUpdateNoOp.ID)

	// Uninstall the app.
	uninstallApp(t, appID)

	// Updating the installation should no longer be allowed.
	updateAppExpectError(t, appID, impl.ErrInvalidOperation.ID)

	// Reverting the installation should no longer be allowed.
	revertAppExpectError(t, appID, impl.ErrInvalidOperation.ID)

	// Starting new instances should no longer be allowed.
	startAppExpectError(t, appID, impl.ErrInvalidOperation.ID)

	// Cleanly shut down the device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dms.Expect("dm terminating")
	dms.ExpectEOF()
}

func tryInstall(ctx *context.T) error {
	appsName := "dm//apps"
	stub := device.ApplicationClient(appsName)
	if _, err := stub.Install(ctx, mockApplicationRepoName); err != nil {
		return fmt.Errorf("Install failed: %v", err)
	}
	return nil
}

func startRealBinaryRepository(t *testing.T) func() {
	rootDir, err := binaryimpl.SetupRootDir("")
	if err != nil {
		t.Fatalf("binaryimpl.SetupRootDir failed: %v", err)
	}
	state, err := binaryimpl.NewState(rootDir, "", 3)
	if err != nil {
		t.Fatalf("binaryimpl.NewState failed: %v", err)
	}
	server, _ := mgmttest.NewServer(globalRT)
	name := "realbin"
	if err := server.ServeDispatcher(name, binaryimpl.NewDispatcher(state, nil)); err != nil {
		t.Fatalf("server.ServeDispatcher failed: %v", err)
	}

	tmpdir, err := ioutil.TempDir("", "test-package-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)
	if err := ioutil.WriteFile(filepath.Join(tmpdir, "hello.txt"), []byte("Hello World!"), 0600); err != nil {
		t.Fatalf("ioutil.WriteFile failed: %v", err)
	}
	if err := libbinary.UploadFromDir(globalRT.NewContext(), naming.Join(name, "testpkg"), tmpdir); err != nil {
		t.Fatalf("libbinary.UploadFromDir failed: %v", err)
	}
	return func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("server.Stop failed: %v", err)
		}
		if err := os.RemoveAll(rootDir); err != nil {
			t.Fatalf("os.RemoveAll(%q) failed: %v", rootDir, err)
		}
	}
}

// TestDeviceManagerClaim claims a devicemanager and tests ACL permissions on
// its methods.
func TestDeviceManagerClaim(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	crDir, crEnv := mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "trapp")

	deviceStub := device.DeviceClient("dm/device")
	claimantRT := mgmttest.NewRuntime(t, globalRT, options.RuntimePrincipal{tsecurity.NewPrincipal("claimant")})
	defer claimantRT.Cleanup()
	otherRT := mgmttest.NewRuntime(t, globalRT, options.RuntimePrincipal{tsecurity.NewPrincipal("other")})
	defer otherRT.Cleanup()

	octx := otherRT.NewContext()

	// Devicemanager should have open ACLs before we claim it and so an
	// Install from otherRT should succeed.
	if err := tryInstall(octx); err != nil {
		t.Errorf("Failed to install: %s", err)
	}
	// Claim the devicemanager with claimantRT as <defaultblessing>/mydevice
	if err := deviceStub.Claim(claimantRT.NewContext(), &granter{p: claimantRT.Principal(), extension: "mydevice"}); err != nil {
		t.Fatal(err)
	}

	// Installation should succeed since claimantRT is now the "owner" of
	// the devicemanager.
	appID := installApp(t, claimantRT)

	// otherRT should be unable to install though, since the ACLs have
	// changed now.
	if err := tryInstall(octx); err == nil {
		t.Fatalf("Install should have failed from otherRT")
	}

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	// Start an instance of the app.
	instanceID := startApp(t, appID, claimantRT)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}
	resolve(t, "trapp", 1)
	suspendApp(t, appID, instanceID, claimantRT)

	// TODO(gauthamt): Test that ACLs persist across devicemanager restarts
}

func TestDeviceManagerUpdateACL(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	var (
		idp = tsecurity.NewIDProvider("root")
		// The two "processes"/runtimes which will act as IPC clients to
		// the devicemanager process.
		selfRT  = globalRT
		otherRT = mgmttest.NewRuntime(t, globalRT)
	)
	defer otherRT.Cleanup()
	octx := otherRT.NewContext()
	// By default, selfRT and otherRT will have blessings generated based on
	// the username/machine name running this process. Since these blessings
	// will appear in ACLs, give them recognizable names.
	if err := idp.Bless(selfRT.Principal(), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(otherRT.Principal(), "other"); err != nil {
		t.Fatal(err)
	}

	crDir, crEnv := mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create an envelope for an app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps")

	deviceStub := device.DeviceClient("dm//device")
	acl, etag, err := deviceStub.GetACL(selfRT.NewContext())
	if err != nil {
		t.Fatalf("GetACL failed:%v", err)
	}
	if etag != "default" {
		t.Fatalf("getACL expected:default, got:%v(%v)", etag, acl)
	}

	// Claim the devicemanager as "root/self/mydevice"
	if err := deviceStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "mydevice"}); err != nil {
		t.Fatal(err)
	}
	expectedACL := make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		expectedACL[string(tag)] = access.ACL{In: []security.BlessingPattern{"root/self/mydevice"}}
	}
	var b bytes.Buffer
	if err := expectedACL.WriteTo(&b); err != nil {
		t.Fatalf("Failed to save ACL:%v", err)
	}
	md5hash := md5.Sum(b.Bytes())
	expectedETAG := hex.EncodeToString(md5hash[:])
	if acl, etag, err = deviceStub.GetACL(selfRT.NewContext()); err != nil {
		t.Fatal(err)
	}
	if etag != expectedETAG {
		t.Fatalf("getACL expected:%v(%v), got:%v(%v)", expectedACL, expectedETAG, acl, etag)
	}
	// Install from otherRT should fail, since it does not match the ACL.
	if err := tryInstall(octx); err == nil {
		t.Fatalf("Install should have failed with random identity")
	}
	newACL := make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		newACL.Add("root/other", string(tag))
	}
	if err := deviceStub.SetACL(selfRT.NewContext(), newACL, "invalid"); err == nil {
		t.Fatalf("SetACL should have failed with invalid etag")
	}
	if err := deviceStub.SetACL(selfRT.NewContext(), newACL, etag); err != nil {
		t.Fatal(err)
	}
	// Install should now fail with selfRT, which no longer matches the ACLs
	// but succeed with otherRT, which does.
	if err := tryInstall(selfRT.NewContext()); err == nil {
		t.Errorf("Install should have failed with selfRT since it should no longer match the ACL")
	}
	if err := tryInstall(octx); err != nil {
		t.Error(err)
	}
}

// TestDeviceManagerInstallUninstall verifies the 'self install' and 'uninstall'
// functionality of the device manager: it runs SelfInstall in a child process,
// then runs the executable from the soft link that the installation created.
// This should bring up a functioning device manager.  In the end it runs
// Uninstall and verifies that the installation is gone.
func TestDeviceManagerInstallUninstall(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	testDir, cleanup := setupRootDir(t)
	defer cleanup()

	root := filepath.Join(testDir, "root")
	currLink := filepath.Join(testDir, "current_link")

	// Create an 'envelope' for the device manager that we can pass to the
	// installer, to ensure that the device manager that the installer
	// configures can run. The installer uses a shell script, so we need
	// to get a set of arguments that will work from within the shell
	// script in order for it to start a device manager.
	// We don't need the environment here since that can only
	// be correctly setup in the actual 'installer' command implementation
	// (in this case the shell script) which will inherit its environment
	// when we run it.
	// TODO(caprita): figure out if this is really necessary, hopefully not.
	dmargs, _ := sh.CommandEnvelope(deviceManagerCmd, nil)
	argsForDeviceManager := append([]string{deviceManagerCmd, "--"}, dmargs[1:]...)
	argsForDeviceManager = append(argsForDeviceManager, "dm")

	// Add vars to instruct the installer how to configure the device
	// manager.
	installerEnv := []string{config.RootEnv + "=" + root, config.CurrentLinkEnv + "=" + currLink, config.HelperEnv + "=" + "unused"}
	installerh, installers := mgmttest.RunShellCommand(t, sh, installerEnv, installerCmd, argsForDeviceManager...)
	installers.ExpectEOF()
	installerh.Shutdown(os.Stderr, os.Stderr)

	// CurrLink should now be pointing to a device manager script that
	// can start up a device manager.
	dmh, dms := mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)

	// We need the pid of the child process started by the device manager
	// script above to signal it, not the pid of the script itself.
	// TODO(caprita): the scripts that the device manager generates should
	// progagate signals so you don't have to obtain the pid of the child by
	// reading it from stdout as we do here. The device manager should be
	// able to retain a list of the processes it spawns and be confident
	// that sending a signal to them will also result in that signal being
	// sent to their children and so on.
	pid := mgmttest.ReadPID(t, dms)
	resolve(t, "dm", 1)
	revertDeviceExpectError(t, "dm", impl.ErrUpdateNoOp.ID) // No previous version available.
	syscall.Kill(pid, syscall.SIGINT)

	dms.Expect("dm terminating")
	dms.ExpectEOF()
	dmh.Shutdown(os.Stderr, os.Stderr)

	// Uninstall.
	uninstallerEnv := []string{config.RootEnv + "=" + root, config.CurrentLinkEnv + "=" + currLink, config.HelperEnv + "=" + "unused"}
	uninstallerh, uninstallers := mgmttest.RunShellCommand(t, sh, uninstallerEnv, uninstallerCmd)
	uninstallers.ExpectEOF()
	uninstallerh.Shutdown(os.Stderr, os.Stderr)
	if _, err := os.Stat(currLink); err == nil || !os.IsNotExist(err) {
		t.Fatalf("Stat(%v) returned %v", currLink, err)
	}
	if _, err := os.Stat(root); err == nil || !os.IsNotExist(err) {
		t.Fatalf("Stat(%v) returned %v", root, err)
	}
}

func TestDeviceManagerGlobAndDebug(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	crDir, crEnv := mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := installApp(t)
	install1ID := path.Base(appID)

	// Start an instance of the app.
	instance1ID := startApp(t, appID)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}

	app2ID := installApp(t)
	install2ID := path.Base(app2ID)

	testcases := []struct {
		name, pattern string
		expected      []string
	}{
		{"dm", "...", []string{
			"",
			"apps",
			"apps/google naps",
			"apps/google naps/" + install1ID,
			"apps/google naps/" + install1ID + "/" + instance1ID,
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs/STDERR-<timestamp>",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs/STDOUT-<timestamp>",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs/bin.INFO",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs/bin.<*>.INFO.<timestamp>",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/pprof",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/stats",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/stats/ipc",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/stats/system",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/stats/system/start-time-rfc1123",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/stats/system/start-time-unix",
			"apps/google naps/" + install2ID,
			"device",
		}},
		{"dm/apps", "*", []string{"google naps"}},
		{"dm/apps/google naps", "*", []string{install1ID, install2ID}},
		{"dm/apps/google naps/" + install1ID, "*", []string{instance1ID}},
		{"dm/apps/google naps/" + install1ID + "/" + instance1ID, "*", []string{"logs", "pprof", "stats"}},
		{"dm/apps/google naps/" + install1ID + "/" + instance1ID + "/logs", "*", []string{
			"STDERR-<timestamp>",
			"STDOUT-<timestamp>",
			"bin.INFO",
			"bin.<*>.INFO.<timestamp>",
		}},
		{"dm/apps/google naps/" + install1ID + "/" + instance1ID + "/stats/system", "start-time*", []string{"start-time-rfc1123", "start-time-unix"}},
	}
	logFileTimeStampRE := regexp.MustCompile("(STDOUT|STDERR)-[0-9]+$")
	logFileTrimInfoRE := regexp.MustCompile(`bin\..*\.INFO\.[0-9.-]+$`)
	logFileRemoveErrorFatalWarningRE := regexp.MustCompile("(ERROR|FATAL|WARNING)")
	statsTrimRE := regexp.MustCompile("/stats/(ipc|system(/start-time.*)?)$")
	for _, tc := range testcases {
		results, err := testutil.GlobName(globalRT.NewContext(), tc.name, tc.pattern)
		if err != nil {
			t.Errorf("unexpected glob error for (%q, %q): %v", tc.name, tc.pattern, err)
			continue
		}
		filteredResults := []string{}
		for _, name := range results {
			// Keep only the stats object names that match this RE.
			if strings.Contains(name, "/stats/") && !statsTrimRE.MatchString(name) {
				continue
			}
			// Remove ERROR, WARNING, FATAL log files because
			// they're not consistently there.
			if logFileRemoveErrorFatalWarningRE.MatchString(name) {
				continue
			}
			name = logFileTimeStampRE.ReplaceAllString(name, "$1-<timestamp>")
			name = logFileTrimInfoRE.ReplaceAllString(name, "bin.<*>.INFO.<timestamp>")
			filteredResults = append(filteredResults, name)
		}
		sort.Strings(filteredResults)
		sort.Strings(tc.expected)
		if !reflect.DeepEqual(filteredResults, tc.expected) {
			t.Errorf("unexpected result for (%q, %q). Got %q, want %q", tc.name, tc.pattern, filteredResults, tc.expected)
		}
	}

	// Call Size() on the log file objects.
	files, err := testutil.GlobName(globalRT.NewContext(), "dm", "apps/google naps/"+install1ID+"/"+instance1ID+"/logs/*")
	if err != nil {
		t.Errorf("unexpected glob error: %v", err)
	}
	if want, got := 4, len(files); got < want {
		t.Errorf("Unexpected number of matches. Got %d, want at least %d", got, want)
	}
	for _, file := range files {
		name := naming.Join("dm", file)
		c := logreader.LogFileClient(name)
		if _, err := c.Size(globalRT.NewContext()); err != nil {
			t.Errorf("Size(%q) failed: %v", name, err)
		}
	}

	// Call Value() on some of the stats objects.
	objects, err := testutil.GlobName(globalRT.NewContext(), "dm", "apps/google naps/"+install1ID+"/"+instance1ID+"/stats/system/start-time*")
	if err != nil {
		t.Errorf("unexpected glob error: %v", err)
	}
	if want, got := 2, len(objects); got != want {
		t.Errorf("Unexpected number of matches. Got %d, want %d", got, want)
	}
	for _, obj := range objects {
		name := naming.Join("dm", obj)
		c := stats.StatsClient(name)
		if _, err := c.Value(globalRT.NewContext()); err != nil {
			t.Errorf("Value(%q) failed: %v", name, err)
		}
	}

	// Call CmdLine() on the pprof object.
	{
		name := "dm/apps/google naps/" + install1ID + "/" + instance1ID + "/pprof"
		c := pprof.PProfClient(name)
		v, err := c.CmdLine(globalRT.NewContext())
		if err != nil {
			t.Errorf("CmdLine(%q) failed: %v", name, err)
		}
		if len(v) == 0 {
			t.Fatalf("Unexpected empty cmdline: %v", v)
		}
		if got, want := filepath.Base(v[0]), "bin"; got != want {
			t.Errorf("Unexpected value for argv[0]. Got %v, want %v", got, want)
		}
	}
}

func TestDeviceManagerPackages(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	defer startRealBinaryRepository(t)()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	crDir, crEnv := mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")
	(*envelope).Packages = map[string]string{
		"test": "realbin/testpkg",
	}

	// Install the app.
	appID := installApp(t)

	// Start an instance of the app.
	startApp(t, appID)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}

	// Ask the app to cat a file from the package.
	file := filepath.Join("packages", "test", "hello.txt")
	name := "appV1"
	content, err := cat(name, file)
	if err != nil {
		t.Errorf("cat(%q, %q) failed: %v", name, file, err)
	}
	if expected := "Hello World!"; content != expected {
		t.Errorf("unexpected content: expected %q, got %q", expected, content)
	}
}

func listAndVerifyAssociations(t *testing.T, stub device.DeviceClientMethods, run veyron2.Runtime, expected []device.Association) {
	assocs, err := stub.ListAssociations(run.NewContext())
	if err != nil {
		t.Fatalf("ListAssociations failed %v", err)
	}
	compareAssociations(t, assocs, expected)
}

// TODO(rjkroege): Verify that associations persist across restarts once
// permanent storage is added.
func TestAccountAssociation(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	var (
		idp = tsecurity.NewIDProvider("root")
		// The two "processes"/runtimes which will act as IPC clients to
		// the devicemanager process.
		selfRT  = globalRT
		otherRT = mgmttest.NewRuntime(t, globalRT)
	)
	defer otherRT.Cleanup()
	// By default, selfRT and otherRT will have blessings generated based on
	// the username/machine name running this process. Since these blessings
	// will appear in test expecations, give them readable names.
	if err := idp.Bless(selfRT.Principal(), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(otherRT.Principal(), "other"); err != nil {
		t.Fatal(err)
	}
	crFile, crEnv := mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crFile)

	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	deviceStub := device.DeviceClient("dm//device")

	// Attempt to list associations on the device manager without having
	// claimed it.
	if list, err := deviceStub.ListAssociations(otherRT.NewContext()); err != nil || list != nil {
		t.Fatalf("ListAssociations should fail on unclaimed device manager but did not: %v", err)
	}

	// self claims the device manager.
	if err := deviceStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "alice"}); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	vlog.VI(2).Info("Verify that associations start out empty.")
	listAndVerifyAssociations(t, deviceStub, selfRT, []device.Association(nil))

	if err := deviceStub.AssociateAccount(selfRT.NewContext(), []string{"root/self", "root/other"}, "alice_system_account"); err != nil {
		t.Fatalf("ListAssociations failed %v", err)
	}
	vlog.VI(2).Info("Added association should appear.")
	listAndVerifyAssociations(t, deviceStub, selfRT, []device.Association{
		{
			"root/self",
			"alice_system_account",
		},
		{
			"root/other",
			"alice_system_account",
		},
	})

	if err := deviceStub.AssociateAccount(selfRT.NewContext(), []string{"root/self", "root/other"}, "alice_other_account"); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	vlog.VI(2).Info("Change the associations and the change should appear.")
	listAndVerifyAssociations(t, deviceStub, selfRT, []device.Association{
		{
			"root/self",
			"alice_other_account",
		},
		{
			"root/other",
			"alice_other_account",
		},
	})

	if err := deviceStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, ""); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	vlog.VI(2).Info("Verify that we can remove an association.")
	listAndVerifyAssociations(t, deviceStub, selfRT, []device.Association{
		{
			"root/self",
			"alice_other_account",
		},
	})
}

// userName is a helper function to determine the system name that the test is
// running under.
func userName(t *testing.T) string {
	u, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current() failed: %v", err)
	}
	return u.Username
}

func TestAppWithSuidHelper(t *testing.T) {
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, globalRT)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	var (
		idp = tsecurity.NewIDProvider("root")
		// The two "processes"/runtimes which will act as IPC clients to
		// the devicemanager process.
		selfRT  = globalRT
		otherRT = mgmttest.NewRuntime(t, globalRT)
	)
	defer otherRT.Cleanup()

	// By default, selfRT and otherRT will have blessings generated based on
	// the username/machine name running this process. Since these blessings
	// can appear in debugging output, give them recognizable names.
	if err := idp.Bless(selfRT.Principal(), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(otherRT.Principal(), "other"); err != nil {
		t.Fatal(err)
	}

	crDir, crEnv := mgmttest.CredentialsForChild(globalRT, "devicemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "-mocksetuid", "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	deviceStub := device.DeviceClient("dm//device")

	// Create the local server that the app uses to tell us which system
	// name the device manager wished to run it as.
	server, _ := mgmttest.NewServer(globalRT)
	defer server.Stop()
	pingCh := make(chan string, 1)
	if err := server.Serve("pingserver", pingServer(pingCh), nil); err != nil {
		t.Fatalf("Serve(%q, <dispatcher>) failed: %v", "pingserver", err)
	}

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install and start the app as root/self.
	appID := installApp(t, selfRT)

	// Claim the devicemanager with selfRT as root/self/alice
	if err := deviceStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "alice"}); err != nil {
		t.Fatal(err)
	}

	// Start an instance of the app but this time it should fail: we do not
	// have an associated uname for the invoking identity.
	startAppExpectError(t, appID, verror.NoAccess.ID, selfRT)

	// Create an association for selfRT
	if err := deviceStub.AssociateAccount(selfRT.NewContext(), []string{"root/self"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	instance1ID := startApp(t, appID, selfRT)
	verifyHelperArgs(t, pingCh, testUserName) // Wait until the app pings us that it's ready.
	stopApp(t, appID, instance1ID, selfRT)

	vlog.VI(2).Infof("other attempting to run an app without access. Should fail.")
	startAppExpectError(t, appID, verror.NoAccess.ID, otherRT)

	// Self will now let other also install apps.
	if err := deviceStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	// Add Start to the ACL list for root/other.
	newACL, _, err := deviceStub.GetACL(selfRT.NewContext())
	if err != nil {
		t.Fatalf("GetACL failed %v", err)
	}
	newACL.Add("root/other", string(access.Write))
	if err := deviceStub.SetACL(selfRT.NewContext(), newACL, ""); err != nil {
		t.Fatalf("SetACL failed %v", err)
	}

	// With the introduction of per installation and per instance ACLs,
	// while other now has administrator permissions on the device manager,
	// other doesn't have execution permissions for the app. So this will
	// fail.
	vlog.VI(2).Infof("other attempting to run an app still without access. Should fail.")
	startAppExpectError(t, appID, verror.NoAccess.ID, otherRT)

	// But self can give other permissions  to start applications.
	vlog.VI(2).Infof("self attempting to give other permission to start %s", appID)
	newACL, _, err = appStub(appID).GetACL(selfRT.NewContext())
	if err != nil {
		t.Fatalf("GetACL on appID: %v failed %v", appID, err)
	}
	newACL.Add("root/other", string(access.Read))
	if err = appStub(appID).SetACL(selfRT.NewContext(), newACL, ""); err != nil {
		t.Fatalf("SetACL on appID: %v failed: %v", appID, err)
	}

	vlog.VI(2).Infof("other attempting to run an app with access. Should succeed.")
	instance2ID := startApp(t, appID, otherRT)
	verifyHelperArgs(t, pingCh, testUserName) // Wait until the app pings us that it's ready.
	suspendApp(t, appID, instance2ID, otherRT)

	vlog.VI(2).Infof("Verify that Resume with the same systemName works.")
	resumeApp(t, appID, instance2ID, otherRT)
	verifyHelperArgs(t, pingCh, testUserName) // Wait until the app pings us that it's ready.
	suspendApp(t, appID, instance2ID, otherRT)

	vlog.VI(2).Infof("Verify that other can install and run applications.")
	otherAppID := installApp(t, otherRT)

	vlog.VI(2).Infof("other attempting to run an app that other installed. Should succeed.")
	instance4ID := startApp(t, otherAppID, otherRT)
	verifyHelperArgs(t, pingCh, testUserName) // Wait until the app pings us that it's ready.

	// Clean up.
	stopApp(t, otherAppID, instance4ID, otherRT)

	// Change the associated system name.
	if err := deviceStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, anotherTestUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	vlog.VI(2).Infof("Show that Resume with a different systemName fails.")
	resumeAppExpectError(t, appID, instance2ID, verror.NoAccess.ID, otherRT)

	// Clean up.
	stopApp(t, appID, instance2ID, otherRT)

	vlog.VI(2).Infof("Show that Start with different systemName works.")
	instance3ID := startApp(t, appID, otherRT)
	verifyHelperArgs(t, pingCh, anotherTestUserName) // Wait until the app pings us that it's ready.

	// Clean up.
	stopApp(t, appID, instance3ID, otherRT)
}
