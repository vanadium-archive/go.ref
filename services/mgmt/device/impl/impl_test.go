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
	"flag"
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
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/device"
	"v.io/core/veyron2/services/mgmt/logreader"
	"v.io/core/veyron2/services/mgmt/pprof"
	"v.io/core/veyron2/services/mgmt/stats"
	"v.io/core/veyron2/services/security/access"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/expect"
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
	// Modules command names.
	execScriptCmd    = "execScriptCmd"
	deviceManagerCmd = "deviceManager"
	appCmd           = "app"
	installerCmd     = "installer"
	uninstallerCmd   = "uninstaller"

	testFlagName = "random_test_flag"
	// VEYRON prefix is necessary to pass the env filtering.
	testEnvVarName = "VEYRON_RANDOM_ENV_VALUE"
)

var flagValue = flag.String(testFlagName, "default", "")

func init() {
	// The installer sets this flag on the installed device manager, so we
	// need to ensure it's defined.
	flag.String("name", "", "")

	modules.RegisterChild(execScriptCmd, "", execScript)
	modules.RegisterChild(deviceManagerCmd, "", deviceManager)
	modules.RegisterChild(appCmd, "", app)
	testutil.Init()

	if modules.IsModulesProcess() {
		return
	}
}

// TestHelperProcess is the entrypoint for the modules commands in a
// a test subprocess.
func TestHelperProcess(t *testing.T) {
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
	ctx, shutdown := veyron2.Init()

	args = args[1:]
	if len(args) == 0 {
		vlog.Fatalf("deviceManager expected at least an argument")
	}
	publishName := args[0]
	args = args[1:]
	defer fmt.Fprintf(stdout, "%v terminated\n", publishName)
	defer vlog.VI(1).Infof("%v terminated", publishName)
	defer shutdown()
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	server, endpoint := mgmttest.NewServer(ctx)
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
	dispatcher, err := impl.NewDispatcher(veyron2.GetPrincipal(ctx), configState, func() { fmt.Println("restart handler") })
	if err != nil {
		vlog.Fatalf("Failed to create device manager dispatcher: %v", err)
	}
	if err := server.ServeDispatcher(publishName, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}
	impl.InvokeCallback(ctx, name)

	fmt.Fprintf(stdout, "ready:%d\n", os.Getpid())

	<-signals.ShutdownOnSignals(ctx)

	if val, present := env["PAUSE_BEFORE_STOP"]; present && val == "1" {
		modules.WaitForEOF(stdin)
	}
	if dispatcher.Leaking() {
		vlog.Fatalf("device manager leaking resources")
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

type pingArgs struct {
	Username, FlagValue, EnvValue string
}

func ping(ctx *context.T) {
	helperEnv := os.Getenv(suidhelper.SavedArgs)
	d := json.NewDecoder(strings.NewReader(helperEnv))
	var savedArgs suidhelper.ArgsSavedForTest
	if err := d.Decode(&savedArgs); err != nil {
		vlog.Fatalf("Failed to decode preserved argument %v: %v", helperEnv, err)
	}
	args := &pingArgs{
		// TODO(rjkroege): Consider validating additional parameters
		// from helper.
		Username:  savedArgs.Uname,
		FlagValue: *flagValue,
		EnvValue:  os.Getenv(testEnvVarName),
	}
	client := veyron2.GetClient(ctx)
	if call, err := client.StartCall(ctx, "pingserver", "Ping", []interface{}{args}); err != nil {
		vlog.Fatalf("StartCall failed: %v", err)
	} else if err := call.Finish(); err != nil {
		vlog.Fatalf("Finish failed: %v", err)
	}
}

func cat(ctx *context.T, name, file string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "Cat", []interface{}{file})
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	args = args[1:]
	if expected, got := 1, len(args); expected != got {
		vlog.Fatalf("Unexpected number of arguments: expected %d, got %d", expected, got)
	}
	publishName := args[0]

	server, _ := mgmttest.NewServer(ctx)
	defer server.Stop()
	if err := server.Serve(publishName, new(appService), nil); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}
	// Some of our tests look for log files, so make sure they are flushed
	// to ensure that at least the files exist.
	vlog.FlushLog()
	ping(ctx)

	<-signals.ShutdownOnSignals(ctx)
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, veyron2.GetPrincipal(ctx))
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	// Current link does not have to live in the root dir, but it's
	// convenient to put it there so we have everything in one place.
	currLink := filepath.Join(root, "current_link")

	dmArgs := []string{"factoryDM", root, "unused_helper", mockApplicationRepoName, currLink}
	args, env := sh.CommandEnvelope(deviceManagerCmd, nil, dmArgs...)

	scriptPathFactory := generateDeviceManagerScript(t, root, args, env)

	if err := os.Symlink(scriptPathFactory, currLink); err != nil {
		t.Fatalf("Symlink(%q, %q) failed: %v", scriptPathFactory, currLink, err)
	}

	// We instruct the initial device manager that we run to pause before
	// stopping its service, so that we get a chance to verify that
	// attempting an update while another one is ongoing will fail.
	dmPauseBeforeStopEnv := []string{"PAUSE_BEFORE_STOP=1"}

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
	resolve(t, ctx, "factoryDM", 1) // Verify the device manager has published itself.

	// Simulate an invalid envelope in the application repository.
	*envelope = envelopeFromShell(sh, dmPauseBeforeStopEnv, deviceManagerCmd, "bogus", dmArgs...)

	updateDeviceExpectError(t, ctx, "factoryDM", impl.ErrAppTitleMismatch.ID)
	revertDeviceExpectError(t, ctx, "factoryDM", impl.ErrUpdateNoOp.ID)

	// Set up a second version of the device manager. The information in the
	// envelope will be used by the device manager to stage the next
	// version.
	crDir, crEnv := mgmttest.CredentialsForChild(ctx, "child")
	defer os.RemoveAll(crDir)
	*envelope = envelopeFromShell(sh, crEnv, deviceManagerCmd, application.DeviceManagerTitle, "v2DM")
	updateDevice(t, ctx, "factoryDM")

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

	updateDeviceExpectError(t, ctx, "factoryDM", impl.ErrOperationInProgress.ID)

	dmh.CloseStdin()

	dms.Expect("restart handler")
	dms.Expect("factoryDM terminated")
	dmh.Shutdown(os.Stderr, os.Stderr)

	// A successful update means the device manager has stopped itself.  We
	// relaunch it from the current link.
	resolveExpectNotFound(t, ctx, "v2DM") // Ensure a clean slate.

	dmh, dms = mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)

	mgmttest.ReadPID(t, dms)
	resolve(t, ctx, "v2DM", 1) // Current link should have been launching v2.

	// Try issuing an update without changing the envelope in the
	// application repository: this should fail, and current link should be
	// unchanged.
	updateDeviceExpectError(t, ctx, "v2DM", impl.ErrUpdateNoOp.ID)
	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Create a third version of the device manager and issue an update.
	crDir, crEnv = mgmttest.CredentialsForChild(ctx, "child")
	defer os.RemoveAll(crDir)
	*envelope = envelopeFromShell(sh, crEnv, deviceManagerCmd, application.DeviceManagerTitle, "v3DM")
	updateDevice(t, ctx, "v2DM")

	scriptPathV3 := evalLink()
	if scriptPathV3 == scriptPathV2 {
		t.Fatalf("current link didn't change")
	}

	dms.Expect("restart handler")
	dms.Expect("v2DM terminated")

	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, ctx, "v3DM") // Ensure a clean slate.

	// Re-lanuch the device manager from current link.  We instruct the
	// device manager to pause before stopping its server, so that we can
	// verify that a second revert fails while a revert is in progress.
	dmh, dms = mgmttest.RunShellCommand(t, sh, dmPauseBeforeStopEnv, execScriptCmd, currLink)

	mgmttest.ReadPID(t, dms)
	resolve(t, ctx, "v3DM", 1) // Current link should have been launching v3.

	// Revert the device manager to its previous version (v2).
	revertDevice(t, ctx, "v3DM")
	revertDeviceExpectError(t, ctx, "v3DM", impl.ErrOperationInProgress.ID) // Revert already in progress.
	dmh.CloseStdin()
	dms.Expect("restart handler")
	dms.Expect("v3DM terminated")
	if evalLink() != scriptPathV2 {
		t.Fatalf("current link was not reverted correctly")
	}
	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, ctx, "v2DM") // Ensure a clean slate.

	dmh, dms = mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dms)
	resolve(t, ctx, "v2DM", 1) // Current link should have been launching v2.

	// Revert the device manager to its previous version (factory).
	revertDevice(t, ctx, "v2DM")
	dms.Expect("restart handler")
	dms.Expect("v2DM terminated")
	if evalLink() != scriptPathFactory {
		t.Fatalf("current link was not reverted correctly")
	}
	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, ctx, "factoryDM") // Ensure a clean slate.

	dmh, dms = mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dms)
	resolve(t, ctx, "factoryDM", 1) // Current link should have been launching factory version.
	stopDevice(t, ctx, "factoryDM")
	dms.Expect("factoryDM terminated")
	dms.ExpectEOF()

	// Re-launch the device manager, to exercise the behavior of Suspend.
	resolveExpectNotFound(t, ctx, "factoryDM") // Ensure a clean slate.
	dmh, dms = mgmttest.RunShellCommand(t, sh, nil, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dms)
	resolve(t, ctx, "factoryDM", 1)
	suspendDevice(t, ctx, "factoryDM")
	dms.Expect("restart handler")
	dms.Expect("factoryDM terminated")
	dms.ExpectEOF()
}

type pingServer chan<- pingArgs

// TODO(caprita): Set the timeout in a more principled manner.
const pingTimeout = 60 * time.Second

func (p pingServer) Ping(_ ipc.ServerContext, arg pingArgs) {
	p <- arg
}

// setupPingServer creates a server listening for a ping from a child app; it
// returns a channel on which the app's ping message is returned, and a cleanup
// function.
func setupPingServer(t *testing.T, ctx *context.T) (<-chan pingArgs, func()) {
	server, _ := mgmttest.NewServer(ctx)
	pingCh := make(chan pingArgs, 1)
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

func verifyPingArgs(t *testing.T, pingCh <-chan pingArgs, username, flagValue, envValue string) {
	var args pingArgs
	select {
	case args = <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf(testutil.FormatLogLine(2, "failed to get ping"))
	}
	wantArgs := pingArgs{
		Username:  username,
		FlagValue: flagValue,
		EnvValue:  envValue,
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf(testutil.FormatLogLine(2, "got ping args %q, expected %q", args, wantArgs))
	}
}

// TestAppLifeCycle installs an app, starts it, suspends it, resumes it, and
// then stops it.
func TestAppLifeCycle(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	crDir, crEnv := mgmttest.CredentialsForChild(ctx, "devicemanager")
	defer os.RemoveAll(crDir)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dms)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	resolve(t, ctx, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, []string{testEnvVarName + "=env-val-envelope"}, appCmd, "google naps", fmt.Sprintf("--%s=flag-val-envelope", testFlagName), "appV1")

	// Install the app.  The config-specified flag value for testFlagName
	// should override the value specified in the envelope above.
	appID := installApp(t, ctx, device.Config{testFlagName: "flag-val-install"})

	// Start requires the caller to grant a blessing for the app instance.
	if _, err := startAppImpl(t, ctx, appID, ""); err == nil || !verror.Is(err, impl.ErrInvalidBlessing.ID) {
		t.Fatalf("Start(%v) expected to fail with %v, got %v instead", appID, impl.ErrInvalidBlessing.ID, err)
	}

	// Start an instance of the app.
	instance1ID := startApp(t, ctx, appID)

	// Wait until the app pings us that it's ready.
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope")

	v1EP1 := resolve(t, ctx, "appV1", 1)[0]

	// Suspend the app instance.
	suspendApp(t, ctx, appID, instance1ID)
	resolveExpectNotFound(t, ctx, "appV1")

	resumeApp(t, ctx, appID, instance1ID)
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.
	oldV1EP1 := v1EP1
	if v1EP1 = resolve(t, ctx, "appV1", 1)[0]; v1EP1 == oldV1EP1 {
		t.Fatalf("Expected a new endpoint for the app after suspend/resume")
	}

	// Start a second instance.
	instance2ID := startApp(t, ctx, appID)
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.

	// There should be two endpoints mounted as "appV1", one for each
	// instance of the app.
	endpoints := resolve(t, ctx, "appV1", 2)
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
	suspendApp(t, ctx, appID, instance1ID)
	// Only the second instance should still be running and mounted.
	if want, got := v1EP2, resolve(t, ctx, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Updating the installation to itself is a no-op.
	updateAppExpectError(t, ctx, appID, impl.ErrUpdateNoOp.ID)

	// Updating the installation should not work with a mismatched title.
	*envelope = envelopeFromShell(sh, nil, appCmd, "bogus")

	updateAppExpectError(t, ctx, appID, impl.ErrAppTitleMismatch.ID)

	// Create a second version of the app and update the app to it.
	*envelope = envelopeFromShell(sh, []string{testEnvVarName + "=env-val-envelope"}, appCmd, "google naps", "appV2")

	updateApp(t, ctx, appID)

	// Second instance should still be running.
	if want, got := v1EP2, resolve(t, ctx, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Resume first instance.
	resumeApp(t, ctx, appID, instance1ID)
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.
	// Both instances should still be running the first version of the app.
	// Check that the mounttable contains two endpoints, one of which is
	// v1EP2.
	endpoints = resolve(t, ctx, "appV1", 2)
	if endpoints[0] == v1EP2 {
		if endpoints[1] == v1EP2 {
			t.Fatalf("Both endpoints are the same")
		}
	} else if endpoints[1] != v1EP2 {
		t.Fatalf("Second endpoint should have been v1EP2: %v, %v", endpoints, v1EP2)
	}

	// Stop first instance.
	stopApp(t, ctx, appID, instance1ID)
	verifyAppWorkspace(t, root, appID, instance1ID)

	// Only second instance is still running.
	if want, got := v1EP2, resolve(t, ctx, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Start a third instance.
	instance3ID := startApp(t, ctx, appID)
	// Wait until the app pings us that it's ready.
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope")

	resolve(t, ctx, "appV2", 1)

	// Stop second instance.
	stopApp(t, ctx, appID, instance2ID)
	resolveExpectNotFound(t, ctx, "appV1")

	// Stop third instance.
	stopApp(t, ctx, appID, instance3ID)
	resolveExpectNotFound(t, ctx, "appV2")

	// Revert the app.
	revertApp(t, ctx, appID)

	// Start a fourth instance.  It should be started from version 1.
	instance4ID := startApp(t, ctx, appID)
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.
	resolve(t, ctx, "appV1", 1)
	stopApp(t, ctx, appID, instance4ID)
	resolveExpectNotFound(t, ctx, "appV1")

	// We are already on the first version, no further revert possible.
	revertAppExpectError(t, ctx, appID, impl.ErrUpdateNoOp.ID)

	// Uninstall the app.
	uninstallApp(t, ctx, appID)

	// Updating the installation should no longer be allowed.
	updateAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Reverting the installation should no longer be allowed.
	revertAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Starting new instances should no longer be allowed.
	startAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Cleanly shut down the device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dms.Expect("dm terminated")
	dms.ExpectEOF()
}

func startRealBinaryRepository(t *testing.T, ctx *context.T) func() {
	rootDir, err := binaryimpl.SetupRootDir("")
	if err != nil {
		t.Fatalf("binaryimpl.SetupRootDir failed: %v", err)
	}
	state, err := binaryimpl.NewState(rootDir, "", 3)
	if err != nil {
		t.Fatalf("binaryimpl.NewState failed: %v", err)
	}
	server, _ := mgmttest.NewServer(ctx)
	name := "realbin"
	d, err := binaryimpl.NewDispatcher(veyron2.GetPrincipal(ctx), state)
	if err != nil {
		t.Fatalf("server.NewDispatcher failed: %v", err)
	}
	if err := server.ServeDispatcher(name, d); err != nil {
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
	if err := libbinary.UploadFromDir(ctx, naming.Join(name, "testpkg"), tmpdir); err != nil {
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	crDir, crEnv := mgmttest.CredentialsForChild(ctx, "devicemanager")
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

	claimantCtx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("claimant"))
	if err != nil {
		t.Fatalf("Could not create claimant principal: %v", err)
	}
	octx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("other"))
	if err != nil {
		t.Fatalf("Could not create other principal: %v", err)
	}

	// Devicemanager should have open ACLs before we claim it and so an
	// Install from octx should succeed.
	installApp(t, octx)
	// Claim the devicemanager with claimantRT as <defaultblessing>/mydevice
	if err := deviceStub.Claim(claimantCtx, &granter{p: veyron2.GetPrincipal(claimantCtx), extension: "mydevice"}); err != nil {
		t.Fatal(err)
	}

	// Installation should succeed since claimantRT is now the "owner" of
	// the devicemanager.
	appID := installApp(t, claimantCtx)

	// octx should be unable to install though, since the ACLs have
	// changed now.
	installAppExpectError(t, octx, verror.NoAccess.ID)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	// Start an instance of the app.
	instanceID := startApp(t, claimantCtx, appID)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}
	resolve(t, ctx, "trapp", 1)
	suspendApp(t, claimantCtx, appID, instanceID)

	// TODO(gauthamt): Test that ACLs persist across devicemanager restarts
}

func TestDeviceManagerUpdateACL(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	// The two "processes"/runtimes which will act as IPC clients to
	// the devicemanager process.
	selfCtx := ctx
	octx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal())
	if err != nil {
		t.Fatalf("Could not create other principal: %v", err)
	}

	// By default, selfCtx and octx will have blessings generated based on
	// the username/machine name running this process. Since these blessings
	// will appear in ACLs, give them recognizable names.
	idp := tsecurity.NewIDProvider("root")
	if err := idp.Bless(veyron2.GetPrincipal(selfCtx), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(veyron2.GetPrincipal(octx), "other"); err != nil {
		t.Fatal(err)
	}

	crDir, crEnv := mgmttest.CredentialsForChild(ctx, "devicemanager")
	defer os.RemoveAll(crDir)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create an envelope for an app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps")

	// On an unclaimed device manager, there will be no ACLs.
	deviceStub := device.DeviceClient("dm/device")
	if _, _, err := deviceStub.GetACL(selfCtx); err == nil {
		t.Fatalf("GetACL should have failed but didn't.")
	}

	// Claim the devicemanager as "root/self/mydevice"
	if err := deviceStub.Claim(selfCtx, &granter{p: veyron2.GetPrincipal(selfCtx), extension: "mydevice"}); err != nil {
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
	acl, etag, err := deviceStub.GetACL(selfCtx)
	if err != nil {
		t.Fatal(err)
	}
	if etag != expectedETAG {
		t.Fatalf("getACL expected:%v(%v), got:%v(%v)", expectedACL, expectedETAG, acl, etag)
	}
	// Install from octx should fail, since it does not match the ACL.
	installAppExpectError(t, octx, verror.NoAccess.ID)

	newACL := make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		newACL.Add("root/other", string(tag))
	}
	if err := deviceStub.SetACL(selfCtx, newACL, "invalid"); err == nil {
		t.Fatalf("SetACL should have failed with invalid etag")
	}
	if err := deviceStub.SetACL(selfCtx, newACL, etag); err != nil {
		t.Fatal(err)
	}
	// Install should now fail with selfCtx, which no longer matches the
	// ACLs but succeed with octx, which does.
	installAppExpectError(t, selfCtx, verror.NoAccess.ID)
	installApp(t, octx)
}

type simpleRW chan []byte

func (s simpleRW) Write(p []byte) (n int, err error) {
	s <- p
	return len(p), nil
}
func (s simpleRW) Read(p []byte) (n int, err error) {
	return copy(p, <-s), nil
}

// TestDeviceManagerInstallation verifies the 'self install' and 'uninstall'
// functionality of the device manager: it runs SelfInstall in a child process,
// then runs the executable from the soft link that the installation created.
// This should bring up a functioning device manager.  In the end it runs
// Uninstall and verifies that the installation is gone.
func TestDeviceManagerInstallation(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()
	testDir, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	// Create a script wrapping the test target that implements suidhelper.
	suidHelperPath := generateSuidHelperScript(t, testDir)
	// Create a dummy script mascarading as the security agent.
	agentPath := generateAgentScript(t, testDir)
	initHelperPath := ""

	// Create an 'envelope' for the device manager that we can pass to the
	// installer, to ensure that the device manager that the installer
	// configures can run.
	dmargs, dmenv := sh.CommandEnvelope(deviceManagerCmd, nil, "dm")
	dmDir := filepath.Join(testDir, "dm")
	// TODO(caprita): Add test logic when initMode = true.
	singleUser, sessionMode, initMode := true, true, false
	if err := impl.SelfInstall(dmDir, suidHelperPath, agentPath, initHelperPath, singleUser, sessionMode, initMode, dmargs[1:], dmenv, os.Stderr, os.Stdout); err != nil {
		t.Fatalf("SelfInstall failed: %v", err)
	}

	resolveExpectNotFound(t, ctx, "dm")
	// Start the device manager.
	stdout := make(simpleRW, 100)
	if err := impl.Start(dmDir, os.Stderr, stdout); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	dms := expect.NewSession(t, stdout, mgmttest.ExpectTimeout)
	mgmttest.ReadPID(t, dms)
	resolve(t, ctx, "dm", 1)
	revertDeviceExpectError(t, ctx, "dm", impl.ErrUpdateNoOp.ID) // No previous version available.

	// Stop the device manager.
	if err := impl.Stop(ctx, dmDir, os.Stderr, os.Stdout); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	dms.Expect("dm terminated")

	// Uninstall.
	if err := impl.Uninstall(dmDir, os.Stderr, os.Stdout); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}
	// Ensure that the installation is gone.
	if files, err := ioutil.ReadDir(dmDir); err != nil || len(files) > 0 {
		var finfo []string
		for _, f := range files {
			finfo = append(finfo, f.Name())
		}
		t.Fatalf("ReadDir returned (%v, %v)", err, finfo)
	}
}

func TestDeviceManagerGlobAndDebug(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	crDir, crEnv := mgmttest.CredentialsForChild(ctx, "devicemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := installApp(t, ctx)
	install1ID := path.Base(appID)

	// Start an instance of the app.
	instance1ID := startApp(t, ctx, appID)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}

	app2ID := installApp(t, ctx)
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
		results, err := testutil.GlobName(ctx, tc.name, tc.pattern)
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
	files, err := testutil.GlobName(ctx, "dm", "apps/google naps/"+install1ID+"/"+instance1ID+"/logs/*")
	if err != nil {
		t.Errorf("unexpected glob error: %v", err)
	}
	if want, got := 4, len(files); got < want {
		t.Errorf("Unexpected number of matches. Got %d, want at least %d", got, want)
	}
	for _, file := range files {
		name := naming.Join("dm", file)
		c := logreader.LogFileClient(name)
		if _, err := c.Size(ctx); err != nil {
			t.Errorf("Size(%q) failed: %v", name, err)
		}
	}

	// Call Value() on some of the stats objects.
	objects, err := testutil.GlobName(ctx, "dm", "apps/google naps/"+install1ID+"/"+instance1ID+"/stats/system/start-time*")
	if err != nil {
		t.Errorf("unexpected glob error: %v", err)
	}
	if want, got := 2, len(objects); got != want {
		t.Errorf("Unexpected number of matches. Got %d, want %d", got, want)
	}
	for _, obj := range objects {
		name := naming.Join("dm", obj)
		c := stats.StatsClient(name)
		if _, err := c.Value(ctx); err != nil {
			t.Errorf("Value(%q) failed: %v", name, err)
		}
	}

	// Call CmdLine() on the pprof object.
	{
		name := "dm/apps/google naps/" + install1ID + "/" + instance1ID + "/pprof"
		c := pprof.PProfClient(name)
		v, err := c.CmdLine(ctx)
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	defer startRealBinaryRepository(t, ctx)()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	crDir, crEnv := mgmttest.CredentialsForChild(ctx, "devicemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")
	(*envelope).Packages = map[string]string{
		"test": "realbin/testpkg",
	}

	// Install the app.
	appID := installApp(t, ctx)

	// Start an instance of the app.
	startApp(t, ctx, appID)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}

	// Ask the app to cat a file from the package.
	file := filepath.Join("packages", "test", "hello.txt")
	name := "appV1"
	content, err := cat(ctx, name, file)
	if err != nil {
		t.Errorf("cat(%q, %q) failed: %v", name, file, err)
	}
	if expected := "Hello World!"; content != expected {
		t.Errorf("unexpected content: expected %q, got %q", expected, content)
	}
}

func listAndVerifyAssociations(t *testing.T, ctx *context.T, stub device.DeviceClientMethods, expected []device.Association) {
	assocs, err := stub.ListAssociations(ctx)
	if err != nil {
		t.Fatalf("ListAssociations failed %v", err)
	}
	compareAssociations(t, assocs, expected)
}

// TODO(rjkroege): Verify that associations persist across restarts once
// permanent storage is added.
func TestAccountAssociation(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	// The two "processes"/contexts which will act as IPC clients to
	// the devicemanager process.
	selfCtx := ctx
	otherCtx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal())
	if err != nil {
		t.Fatalf("Could not create other principal: %v", err)
	}

	// By default, selfCtx and otherCtx will have blessings generated based
	// on the username/machine name running this process. Since these
	// blessings will appear in test expecations, give them readable names.
	idp := tsecurity.NewIDProvider("root")
	if err := idp.Bless(veyron2.GetPrincipal(selfCtx), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(veyron2.GetPrincipal(otherCtx), "other"); err != nil {
		t.Fatal(err)
	}
	crFile, crEnv := mgmttest.CredentialsForChild(ctx, "devicemanager")
	defer os.RemoveAll(crFile)

	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "dm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	deviceStub := device.DeviceClient("dm//device")

	// Attempt to list associations on the device manager without having
	// claimed it.
	if list, err := deviceStub.ListAssociations(otherCtx); err != nil || list != nil {
		t.Fatalf("ListAssociations should fail on unclaimed device manager but did not: %v", err)
	}

	// self claims the device manager.
	if err := deviceStub.Claim(selfCtx, &granter{p: veyron2.GetPrincipal(selfCtx), extension: "alice"}); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	vlog.VI(2).Info("Verify that associations start out empty.")
	listAndVerifyAssociations(t, selfCtx, deviceStub, []device.Association(nil))

	if err := deviceStub.AssociateAccount(selfCtx, []string{"root/self", "root/other"}, "alice_system_account"); err != nil {
		t.Fatalf("ListAssociations failed %v", err)
	}
	vlog.VI(2).Info("Added association should appear.")
	listAndVerifyAssociations(t, selfCtx, deviceStub, []device.Association{
		{
			"root/self",
			"alice_system_account",
		},
		{
			"root/other",
			"alice_system_account",
		},
	})

	if err := deviceStub.AssociateAccount(selfCtx, []string{"root/self", "root/other"}, "alice_other_account"); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	vlog.VI(2).Info("Change the associations and the change should appear.")
	listAndVerifyAssociations(t, selfCtx, deviceStub, []device.Association{
		{
			"root/self",
			"alice_other_account",
		},
		{
			"root/other",
			"alice_other_account",
		},
	})

	if err := deviceStub.AssociateAccount(selfCtx, []string{"root/other"}, ""); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	vlog.VI(2).Info("Verify that we can remove an association.")
	listAndVerifyAssociations(t, selfCtx, deviceStub, []device.Association{
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-principal"))
	if err != nil {
		panic(err)
	}
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()

	// The two "processes"/runtimes which will act as IPC clients to
	// the devicemanager process.
	selfCtx := ctx
	otherCtx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal())
	if err != nil {
		t.Fatalf("Could not create other principal: %v", err)
	}

	// By default, selfCtx and otherCtx will have blessings generated based
	// on the username/machine name running this process. Since these
	// blessings can appear in debugging output, give them recognizable
	// names.
	idp := tsecurity.NewIDProvider("root")
	if err := idp.Bless(veyron2.GetPrincipal(selfCtx), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(veyron2.GetPrincipal(otherCtx), "other"); err != nil {
		t.Fatal(err)
	}

	crDir, crEnv := mgmttest.CredentialsForChild(ctx, "devicemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	_, dms := mgmttest.RunShellCommand(t, sh, crEnv, deviceManagerCmd, "-mocksetuid", "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dms)
	defer syscall.Kill(pid, syscall.SIGINT)

	deviceStub := device.DeviceClient("dm//device")

	// Create the local server that the app uses to tell us which system
	// name the device manager wished to run it as.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, []string{testEnvVarName + "=env-var"}, appCmd, "google naps", fmt.Sprintf("--%s=flag-val-envelope", testFlagName), "appV1")

	// Install and start the app as root/self.
	appID := installApp(t, selfCtx)

	// Claim the devicemanager with selfCtx as root/self/alice
	if err := deviceStub.Claim(selfCtx, &granter{p: veyron2.GetPrincipal(selfCtx), extension: "alice"}); err != nil {
		t.Fatal(err)
	}

	// Start an instance of the app but this time it should fail: we do not
	// have an associated uname for the invoking identity.
	startAppExpectError(t, selfCtx, appID, verror.NoAccess.ID)

	// Create an association for selfCtx
	if err := deviceStub.AssociateAccount(selfCtx, []string{"root/self"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	instance1ID := startApp(t, selfCtx, appID)
	verifyPingArgs(t, pingCh, testUserName, "flag-val-envelope", "env-var") // Wait until the app pings us that it's ready.
	stopApp(t, selfCtx, appID, instance1ID)

	vlog.VI(2).Infof("other attempting to run an app without access. Should fail.")
	startAppExpectError(t, otherCtx, appID, verror.NoAccess.ID)

	// Self will now let other also install apps.
	if err := deviceStub.AssociateAccount(selfCtx, []string{"root/other"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	// Add Start to the ACL list for root/other.
	newACL, _, err := deviceStub.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL failed %v", err)
	}
	newACL.Add("root/other", string(access.Write))
	if err := deviceStub.SetACL(selfCtx, newACL, ""); err != nil {
		t.Fatalf("SetACL failed %v", err)
	}

	// With the introduction of per installation and per instance ACLs,
	// while other now has administrator permissions on the device manager,
	// other doesn't have execution permissions for the app. So this will
	// fail.
	vlog.VI(2).Infof("other attempting to run an app still without access. Should fail.")
	startAppExpectError(t, otherCtx, appID, verror.NoAccess.ID)

	// But self can give other permissions  to start applications.
	vlog.VI(2).Infof("self attempting to give other permission to start %s", appID)
	newACL, _, err = appStub(appID).GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL on appID: %v failed %v", appID, err)
	}
	newACL.Add("root/other", string(access.Read))
	if err = appStub(appID).SetACL(selfCtx, newACL, ""); err != nil {
		t.Fatalf("SetACL on appID: %v failed: %v", appID, err)
	}

	vlog.VI(2).Infof("other attempting to run an app with access. Should succeed.")
	instance2ID := startApp(t, otherCtx, appID)
	verifyPingArgs(t, pingCh, testUserName, "flag-val-envelope", "env-var") // Wait until the app pings us that it's ready.
	suspendApp(t, otherCtx, appID, instance2ID)

	vlog.VI(2).Infof("Verify that Resume with the same systemName works.")
	resumeApp(t, otherCtx, appID, instance2ID)
	verifyPingArgs(t, pingCh, testUserName, "flag-val-envelope", "env-var") // Wait until the app pings us that it's ready.
	suspendApp(t, otherCtx, appID, instance2ID)

	vlog.VI(2).Infof("Verify that other can install and run applications.")
	otherAppID := installApp(t, otherCtx)

	vlog.VI(2).Infof("other attempting to run an app that other installed. Should succeed.")
	instance4ID := startApp(t, otherCtx, otherAppID)
	verifyPingArgs(t, pingCh, testUserName, "flag-val-envelope", "env-var") // Wait until the app pings us that it's ready.

	// Clean up.
	stopApp(t, otherCtx, otherAppID, instance4ID)

	// Change the associated system name.
	if err := deviceStub.AssociateAccount(selfCtx, []string{"root/other"}, anotherTestUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	vlog.VI(2).Infof("Show that Resume with a different systemName fails.")
	resumeAppExpectError(t, otherCtx, appID, instance2ID, verror.NoAccess.ID)

	// Clean up.
	stopApp(t, otherCtx, appID, instance2ID)

	vlog.VI(2).Infof("Show that Start with different systemName works.")
	instance3ID := startApp(t, otherCtx, appID)
	verifyPingArgs(t, pingCh, anotherTestUserName, "flag-val-envelope", "env-var") // Wait until the app pings us that it's ready.

	// Clean up.
	stopApp(t, otherCtx, appID, instance3ID)
}
