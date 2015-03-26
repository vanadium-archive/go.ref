// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO(caprita): This file is becoming unmanageable; split into several test
// files.
// TODO(rjkroege): Add a more extensive unit test case to exercise AccessList logic.

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
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/x/lib/vlog"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/mgmt"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/services/mgmt/device"
	"v.io/v23/services/mgmt/repository"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"

	"v.io/x/ref/lib/flags/consts"
	"v.io/x/ref/lib/signals"
	binaryimpl "v.io/x/ref/services/mgmt/binary/impl"
	"v.io/x/ref/services/mgmt/device/config"
	"v.io/x/ref/services/mgmt/device/impl"
	"v.io/x/ref/services/mgmt/device/starter"
	libbinary "v.io/x/ref/services/mgmt/lib/binary"
	mgmttest "v.io/x/ref/services/mgmt/lib/testutil"
	suidhelper "v.io/x/ref/services/mgmt/suidhelper/impl"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

//go:generate v23 test generate .

const (
	// Modules command names.
	execScriptCmd       = "execScript"
	deviceManagerCmd    = "deviceManager"
	deviceManagerV10Cmd = "deviceManagerV10" // deviceManager with a different major version number
	appCmd              = "app"
	installerCmd        = "installer"
	uninstallerCmd      = "uninstaller"

	testFlagName = "random_test_flag"
	// VEYRON prefix is necessary to pass the env filtering.
	testEnvVarName = "VEYRON_RANDOM_ENV_VALUE"

	redirectEnv = "DEVICE_MANAGER_DONT_REDIRECT_STDOUT_STDERR"

	noPairingToken = ""
)

var flagValue = flag.String(testFlagName, "default", "")

func init() {
	// The installer sets this flag on the installed device manager, so we
	// need to ensure it's defined.
	flag.String("name", "", "")
}

func TestMain(m *testing.M) {
	test.Init()
	isSuidHelper := len(os.Getenv("VEYRON_SUIDHELPER_TEST")) > 0
	if modules.IsModulesChildProcess() && !isSuidHelper {
		if err := modules.Dispatch(); err != nil {
			fmt.Fprintf(os.Stderr, "modules.Dispatch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

// TestSuidHelper is testing boilerplate for suidhelper that does not
// create a runtime because the suidhelper is not a Vanadium application.
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
	if want, got := 1, len(args); want != got {
		vlog.Fatalf("execScript expected %d arguments, got %d instead", want, got)
	}
	script := args[0]
	osenv := []string{redirectEnv + "=1"}
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
	ctx, shutdown := test.InitForTest()
	if len(args) == 0 {
		vlog.Fatalf("deviceManager expected at least an argument")
	}
	publishName := args[0]
	args = args[1:]
	defer fmt.Fprintf(stdout, "%v terminated\n", publishName)
	defer vlog.VI(1).Infof("%v terminated", publishName)
	defer shutdown()
	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	// Satisfy the contract described in doc.go by passing the config state
	// through to the device manager dispatcher constructor.
	configState, err := config.Load()
	if err != nil {
		vlog.Fatalf("Failed to decode config state: %v", err)
	}

	// This exemplifies how to override or set specific config fields, if,
	// for example, the device manager is invoked 'by hand' instead of via a
	// script prepared by a previous version of the device manager.
	var pairingToken string
	if len(args) > 0 {
		if want, got := 4, len(args); want > got {
			vlog.Fatalf("expected atleast %d additional arguments, got %d instead: %q", want, got, args)
		}
		configState.Root, configState.Helper, configState.Origin, configState.CurrentLink = args[0], args[1], args[2], args[3]
		if len(args) > 4 {
			pairingToken = args[4]
		}
	}
	// We grab the shutdown channel at this point in order to ensure that we
	// register a listener for the app cycle manager Stop before we start
	// running the device manager service.  Otherwise, any device manager
	// method that calls Stop on the app cycle manager (e.g. the Stop RPC)
	// will precipitate an immediate process exit.
	shutdownChan := signals.ShutdownOnSignals(ctx)
	stop, err := starter.Start(ctx, starter.Args{
		Namespace: starter.NamespaceArgs{
			ListenSpec: rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}},
		},
		Device: starter.DeviceArgs{
			Name:            publishName,
			ListenSpec:      rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}},
			ConfigState:     configState,
			TestMode:        strings.HasSuffix(fmt.Sprint(v23.GetPrincipal(ctx).BlessingStore().Default()), "/testdm"),
			RestartCallback: func() { fmt.Println("restart handler") },
			PairingToken:    pairingToken,
		},
		// TODO(rthellend): Wire up the local mounttable like the real device
		// manager, i.e. mount the device manager and the apps on it, and mount
		// the local mounttable in the global namespace.
		// MountGlobalNamespaceInLocalNamespace: true,
	})
	if err != nil {
		vlog.Errorf("starter.Start failed: %v", err)
		return err
	}
	defer stop()
	fmt.Fprintf(stdout, "ready:%d\n", os.Getpid())

	<-shutdownChan
	if val, present := env["PAUSE_BEFORE_STOP"]; present && val == "1" {
		modules.WaitForEOF(stdin)
	}
	// TODO(ashankar): Figure out a way to incorporate this check in the test.
	// if impl.DispatcherLeaking(dispatcher) {
	//	vlog.Fatalf("device manager leaking resources")
	// }
	return nil
}

// This is the same as deviceManager above, except that it has a different major version number
func deviceManagerV10(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	impl.CurrentVersion = impl.Version{10, 0} // Set the version number to 10.0
	return deviceManager(stdin, stdout, stderr, env, args...)
}

// appService defines a test service that the test app should be running.
// TODO(caprita): Use this to make calls to the app and verify how Suspend/Stop
// interact with an active service.
type appService struct{}

func (appService) Echo(_ rpc.ServerCall, message string) (string, error) {
	return message, nil
}

func (appService) Cat(_ rpc.ServerCall, file string) (string, error) {
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
	client := v23.GetClient(ctx)
	if call, err := client.StartCall(ctx, "pingserver", "Ping", []interface{}{args}); err != nil {
		vlog.Fatalf("StartCall failed: %v", err)
	} else if err := call.Finish(); err != nil {
		vlog.Fatalf("Finish failed: %v", err)
	}
}

func cat(ctx *context.T, name, file string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	client := v23.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "Cat", []interface{}{file})
	if err != nil {
		return "", err
	}
	var content string
	if err := call.Finish(&content); err != nil {
		return "", err
	}
	return content, nil
}

func app(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

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

func initForTest() (*context.T, v23.Shutdown) {
	os.Unsetenv(consts.NamespaceRootPrefix)
	ctx, shutdown := test.InitForTest()
	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))
	return ctx, shutdown
}

// TestDeviceManagerUpdateAndRevert makes the device manager go through the
// motions of updating itself to newer versions (twice), and reverting itself
// back (twice). It also checks that update and revert fail when they're
// supposed to. The initial device manager is started 'by hand' via a module
// command. Further versions are started through the soft link that the device
// manager itself updates.
func TestDeviceManagerUpdateAndRevert(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, v23.GetPrincipal(ctx))
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Current link does not have to live in the root dir, but it's
	// convenient to put it there so we have everything in one place.
	currLink := filepath.Join(root, "current_link")

	// Since the device manager will be restarted, use the
	// VeyronCredentials environment variable to maintain the same set of
	// credentials across runs.
	// Without this, authentication/authorizatin state - such as the blessings
	// of the device manager and the signatures used for AccessList integrity checks
	// - will not carry over between updates to the binary, which would not
	// be reflective of intended use.
	dmCreds, err := ioutil.TempDir("", "TestDeviceManagerUpdateAndRevert")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dmCreds)
	dmEnv := []string{fmt.Sprintf("%v=%v", consts.VeyronCredentials, dmCreds)}
	dmArgs := []string{"factoryDM", root, "unused_helper", mockApplicationRepoName, currLink}
	args, env := sh.CommandEnvelope(deviceManagerCmd, dmEnv, dmArgs...)
	scriptPathFactory := generateDeviceManagerScript(t, root, args, env)

	if err := os.Symlink(scriptPathFactory, currLink); err != nil {
		t.Fatalf("Symlink(%q, %q) failed: %v", scriptPathFactory, currLink, err)
	}

	// We instruct the initial device manager that we run to pause before
	// stopping its service, so that we get a chance to verify that
	// attempting an update while another one is ongoing will fail.
	dmPauseBeforeStopEnv := append(dmEnv, "PAUSE_BEFORE_STOP=1")

	// Start the initial version of the device manager, the so-called
	// "factory" version. We use the modules-generated command to start it.
	// We could have also used the scriptPathFactory to start it, but this
	// demonstrates that the initial device manager could be started by hand
	// as long as the right initial configuration is passed into the device
	// manager implementation.
	dmh := mgmttest.RunCommand(t, sh, dmPauseBeforeStopEnv, deviceManagerCmd, dmArgs...)
	defer func() {
		syscall.Kill(dmh.Pid(), syscall.SIGINT)
	}()

	mgmttest.ReadPID(t, dmh)
	// Brand new device manager must be claimed first.
	claimDevice(t, ctx, "factoryDM", "mydevice", noPairingToken)

	// Simulate an invalid envelope in the application repository.
	*envelope = envelopeFromShell(sh, dmPauseBeforeStopEnv, deviceManagerCmd, "bogus", dmArgs...)

	updateDeviceExpectError(t, ctx, "factoryDM", impl.ErrAppTitleMismatch.ID)
	revertDeviceExpectError(t, ctx, "factoryDM", impl.ErrUpdateNoOp.ID)

	// Set up a second version of the device manager. The information in the
	// envelope will be used by the device manager to stage the next
	// version.
	*envelope = envelopeFromShell(sh, dmEnv, deviceManagerCmd, application.DeviceManagerTitle, "v2DM")
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

	dmh.Expect("restart handler")
	dmh.Expect("factoryDM terminated")
	dmh.Shutdown(os.Stderr, os.Stderr)

	// A successful update means the device manager has stopped itself.  We
	// relaunch it from the current link.
	resolveExpectNotFound(t, ctx, "v2DM") // Ensure a clean slate.

	dmh = mgmttest.RunCommand(t, sh, dmEnv, execScriptCmd, currLink)

	mgmttest.ReadPID(t, dmh)
	resolve(t, ctx, "v2DM", 1) // Current link should have been launching v2.

	// Try issuing an update without changing the envelope in the
	// application repository: this should fail, and current link should be
	// unchanged.
	updateDeviceExpectError(t, ctx, "v2DM", impl.ErrUpdateNoOp.ID)
	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Try issuing an update with a binary that has a different major version number. It should fail
	resolveExpectNotFound(t, ctx, "v2.5DM") // Ensure a clean slate.
	*envelope = envelopeFromShell(sh, dmEnv, deviceManagerV10Cmd, application.DeviceManagerTitle, "v2.5DM")
	updateDeviceExpectError(t, ctx, "v2DM", impl.ErrOperationFailed.ID)

	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Create a third version of the device manager and issue an update.
	*envelope = envelopeFromShell(sh, dmEnv, deviceManagerCmd, application.DeviceManagerTitle, "v3DM")
	updateDevice(t, ctx, "v2DM")

	scriptPathV3 := evalLink()
	if scriptPathV3 == scriptPathV2 {
		t.Fatalf("current link didn't change")
	}

	dmh.Expect("restart handler")
	dmh.Expect("v2DM terminated")

	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, ctx, "v3DM") // Ensure a clean slate.

	// Re-lanuch the device manager from current link.  We instruct the
	// device manager to pause before stopping its server, so that we can
	// verify that a second revert fails while a revert is in progress.
	dmh = mgmttest.RunCommand(t, sh, dmPauseBeforeStopEnv, execScriptCmd, currLink)

	mgmttest.ReadPID(t, dmh)
	resolve(t, ctx, "v3DM", 1) // Current link should have been launching v3.

	// Revert the device manager to its previous version (v2).
	revertDevice(t, ctx, "v3DM")
	revertDeviceExpectError(t, ctx, "v3DM", impl.ErrOperationInProgress.ID) // Revert already in progress.
	dmh.CloseStdin()
	dmh.Expect("restart handler")
	dmh.Expect("v3DM terminated")
	if evalLink() != scriptPathV2 {
		t.Fatalf("current link was not reverted correctly")
	}
	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, ctx, "v2DM") // Ensure a clean slate.

	dmh = mgmttest.RunCommand(t, sh, dmEnv, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dmh)
	resolve(t, ctx, "v2DM", 1) // Current link should have been launching v2.

	// Revert the device manager to its previous version (factory).
	revertDevice(t, ctx, "v2DM")
	dmh.Expect("restart handler")
	dmh.Expect("v2DM terminated")
	if evalLink() != scriptPathFactory {
		t.Fatalf("current link was not reverted correctly")
	}
	dmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, ctx, "factoryDM") // Ensure a clean slate.

	dmh = mgmttest.RunCommand(t, sh, dmEnv, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dmh)
	resolve(t, ctx, "factoryDM", 1) // Current link should have been launching factory version.
	stopDevice(t, ctx, "factoryDM")
	dmh.Expect("factoryDM terminated")
	dmh.ExpectEOF()

	// Re-launch the device manager, to exercise the behavior of Suspend.
	resolveExpectNotFound(t, ctx, "factoryDM") // Ensure a clean slate.
	dmh = mgmttest.RunCommand(t, sh, dmEnv, execScriptCmd, currLink)
	mgmttest.ReadPID(t, dmh)
	resolve(t, ctx, "factoryDM", 1)
	suspendDevice(t, ctx, "factoryDM")
	dmh.Expect("restart handler")
	dmh.Expect("factoryDM terminated")
	dmh.ExpectEOF()
}

type pingServer chan<- pingArgs

// TODO(caprita): Set the timeout in a more principled manner.
const pingTimeout = 60 * time.Second

func (p pingServer) Ping(_ rpc.ServerCall, arg pingArgs) error {
	p <- arg
	return nil
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

func instanceDirForApp(root, appID, instanceID string) string {
	applicationDirName := func(title string) string {
		h := md5.New()
		h.Write([]byte(title))
		hash := strings.TrimRight(base64.URLEncoding.EncodeToString(h.Sum(nil)), "=")
		return "app-" + hash
	}
	components := strings.Split(appID, "/")
	appTitle, installationID := components[0], components[1]
	return filepath.Join(root, applicationDirName(appTitle), "installation-"+installationID, "instances", "instance-"+instanceID)
}

func verifyAppWorkspace(t *testing.T, root, appID, instanceID string) {
	// HACK ALERT: for now, we peek inside the device manager's directory
	// structure (which ought to be opaque) to check for what the app has
	// written to its local root.
	//
	// TODO(caprita): add support to device manager to browse logs/app local
	// root.
	rootDir := filepath.Join(instanceDirForApp(root, appID, instanceID), "root")
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

// TestLifeOfAnApp installs an app, starts, suspends, resumes, and stops several
// instances, and performs updates.
func TestLifeOfAnApp(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dmh)
	claimDevice(t, ctx, "dm", "mydevice", noPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	resolve(t, ctx, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, []string{testEnvVarName + "=env-val-envelope"}, appCmd, "google naps", fmt.Sprintf("--%s=flag-val-envelope", testFlagName), "appV1")

	// Install the app.  The config-specified flag value for testFlagName
	// should override the value specified in the envelope above, and the
	// config-specified value for origin should override the value in the
	// Install rpc argument.
	mtName, ok := sh.GetVar(consts.NamespaceRootPrefix)
	if !ok {
		t.Fatalf("failed to get namespace root var from shell")
	}
	// This rooted name should be equivalent to the relative name "ar", but
	// we want to test that the config override for origin works.
	rootedAppRepoName := naming.Join(mtName, "ar")
	appID := installApp(t, ctx, device.Config{testFlagName: "flag-val-install", mgmt.AppOriginConfigKey: rootedAppRepoName})
	v1 := verifyState(t, ctx, device.InstallationStateActive, appID)
	installationDebug := debug(t, ctx, appID)
	// We spot-check a couple pieces of information we expect in the debug
	// output.
	// TODO(caprita): Is there a way to verify more without adding brittle
	// logic that assumes too much about the format?  This may be one
	// argument in favor of making the output of Debug a struct instead of
	// free-form string.
	if !strings.Contains(installationDebug, fmt.Sprintf("Origin: %v", rootedAppRepoName)) {
		t.Fatalf("debug response doesn't contain expected info: %v", installationDebug)
	}
	if !strings.Contains(installationDebug, "Config: map[random_test_flag:flag-val-install]") {
		t.Fatalf("debug response doesn't contain expected info: %v", installationDebug)
	}

	// Start requires the caller to bless the app instance.
	expectedErr := "bless failed"
	if _, err := startAppImpl(t, ctx, appID, ""); err == nil || err.Error() != expectedErr {
		t.Fatalf("Start(%v) expected to fail with %v, got %v instead", appID, expectedErr, err)
	}

	// Start an instance of the app.
	instance1ID := startApp(t, ctx, appID)
	if v := verifyState(t, ctx, device.InstanceStateStarted, appID, instance1ID); v != v1 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v1, v)
	}

	instanceDebug := debug(t, ctx, appID, instance1ID)
	if !strings.Contains(instanceDebug, fmt.Sprintf("Blessing Store: Default blessings: %s/forapp", test.TestBlessing)) {
		t.Fatalf("debug response doesn't contain expected info: %v", instanceDebug)
	}

	// Wait until the app pings us that it's ready.
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope")

	v1EP1 := resolve(t, ctx, "appV1", 1)[0]

	// Suspend the app instance.
	suspendApp(t, ctx, appID, instance1ID)
	verifyState(t, ctx, device.InstanceStateSuspended, appID, instance1ID)
	resolveExpectNotFound(t, ctx, "appV1")

	resumeApp(t, ctx, appID, instance1ID)
	verifyState(t, ctx, device.InstanceStateStarted, appID, instance1ID)
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

	// TODO(caprita): verify various non-standard combinations (suspend when
	// stopped; resume while still running; stop while suspended).

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

	v2 := verifyState(t, ctx, device.InstallationStateActive, appID)
	if v1 == v2 {
		t.Fatalf("Version did not change for %v: %v", appID, v1)
	}

	// Second instance should still be running.
	if want, got := v1EP2, resolve(t, ctx, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}
	if v := verifyState(t, ctx, device.InstanceStateStarted, appID, instance2ID); v != v1 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v1, v)
	}

	// Resume first instance.
	resumeApp(t, ctx, appID, instance1ID)
	if v := verifyState(t, ctx, device.InstanceStateStarted, appID, instance1ID); v != v1 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v1, v)
	}
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

	// Trying to update first instance while it's running should fail.
	updateInstanceExpectError(t, ctx, appID, instance1ID, impl.ErrInvalidOperation.ID)
	// Suspend first instance and try again.
	suspendApp(t, ctx, appID, instance1ID)
	// Only the second instance should still be running and mounted.
	if want, got := v1EP2, resolve(t, ctx, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}
	// Update succeeds now.
	updateInstance(t, ctx, appID, instance1ID)
	if v := verifyState(t, ctx, device.InstanceStateSuspended, appID, instance1ID); v != v2 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v2, v)
	}
	// Resume the first instance and verify it's running v2 now.
	resumeApp(t, ctx, appID, instance1ID)
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope")
	resolve(t, ctx, "appV1", 1)
	resolve(t, ctx, "appV2", 1)

	// Stop first instance.
	stopApp(t, ctx, appID, instance1ID)
	verifyAppWorkspace(t, root, appID, instance1ID)
	resolveExpectNotFound(t, ctx, "appV2")

	// Start a third instance.
	instance3ID := startApp(t, ctx, appID)
	if v := verifyState(t, ctx, device.InstanceStateStarted, appID, instance3ID); v != v2 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v2, v)
	}
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
	if v := verifyState(t, ctx, device.InstallationStateActive, appID); v != v1 {
		t.Fatalf("Installation version expected to be %v, got %v instead", v1, v)
	}

	// Start a fourth instance.  It should be started from version 1.
	instance4ID := startApp(t, ctx, appID)
	if v := verifyState(t, ctx, device.InstanceStateStarted, appID, instance4ID); v != v1 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v1, v)
	}
	verifyPingArgs(t, pingCh, userName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.
	resolve(t, ctx, "appV1", 1)
	stopApp(t, ctx, appID, instance4ID)
	resolveExpectNotFound(t, ctx, "appV1")

	// We are already on the first version, no further revert possible.
	revertAppExpectError(t, ctx, appID, impl.ErrUpdateNoOp.ID)

	// Uninstall the app.
	uninstallApp(t, ctx, appID)
	verifyState(t, ctx, device.InstallationStateUninstalled, appID)

	// Updating the installation should no longer be allowed.
	updateAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Reverting the installation should no longer be allowed.
	revertAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Starting new instances should no longer be allowed.
	startAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Cleanly shut down the device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
}

func startRealBinaryRepository(t *testing.T, ctx *context.T, von string) func() {
	rootDir, err := binaryimpl.SetupRootDir("")
	if err != nil {
		t.Fatalf("binaryimpl.SetupRootDir failed: %v", err)
	}
	state, err := binaryimpl.NewState(rootDir, "", 3)
	if err != nil {
		t.Fatalf("binaryimpl.NewState failed: %v", err)
	}
	server, _ := mgmttest.NewServer(ctx)
	d, err := binaryimpl.NewDispatcher(v23.GetPrincipal(ctx), state)
	if err != nil {
		t.Fatalf("server.NewDispatcher failed: %v", err)
	}
	if err := server.ServeDispatcher(von, d); err != nil {
		t.Fatalf("server.ServeDispatcher failed: %v", err)
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

// TestDeviceManagerClaim claims a devicemanager and tests AccessList permissions on
// its methods.
func TestDeviceManagerClaim(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()

	// root blessing provider so that the principals of all the contexts
	// recognize each other.
	idp := testutil.NewIDProvider("root")
	if err := idp.Bless(v23.GetPrincipal(ctx), "ctx"); err != nil {
		t.Fatal(err)
	}

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	pairingToken := "abcxyz"
	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link", pairingToken)
	pid := mgmttest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)

	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "trapp")

	claimantCtx := ctxWithNewPrincipal(t, ctx, idp, "claimant")
	octx, err := v23.SetPrincipal(ctx, testutil.NewPrincipal("other"))
	if err != nil {
		t.Fatal(err)
	}

	// Unclaimed devices cannot do anything but be claimed.
	// TODO(ashankar,caprita): The line below will currently fail with
	// ErrUnclaimedDevice != NotTrusted. NotTrusted can be avoided by
	// passing options.SkipServerEndpointAuthorization{} to the "Install" RPC.
	// Refactor the helper function to make this possible.
	//installAppExpectError(t, octx, impl.ErrUnclaimedDevice.ID)

	// Claim the device with an incorrect pairing token should fail.
	claimDeviceExpectError(t, claimantCtx, "dm", "mydevice", "badtoken", impl.ErrInvalidPairingToken.ID)
	// But succeed with a valid pairing token
	claimDevice(t, claimantCtx, "dm", "mydevice", pairingToken)

	// Installation should succeed since claimantRT is now the "owner" of
	// the devicemanager.
	appID := installApp(t, claimantCtx)

	// octx will not install the app now since it doesn't recognize the
	// device's blessings. The error returned will be ErrNoServers as that
	// is what the IPC stack does when there are no authorized servers.
	installAppExpectError(t, octx, verror.ErrNoServers.ID)
	// Even if it does recognize the device (by virtue of recognizing the
	// claimant), the device will not allow it to install.
	if err := v23.GetPrincipal(octx).AddToRoots(v23.GetPrincipal(claimantCtx).BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}
	installAppExpectError(t, octx, verror.ErrNoAccess.ID)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, claimantCtx)
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

	// TODO(gauthamt): Test that AccessLists persist across devicemanager restarts
}

func TestDeviceManagerUpdateAccessList(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()

	// Identity provider to ensure that all processes recognize each
	// others' blessings.
	idp := testutil.NewIDProvider("root")
	ctx = ctxWithNewPrincipal(t, ctx, idp, "self")

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	selfCtx := ctx
	octx := ctxWithNewPrincipal(t, selfCtx, idp, "other")

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create an envelope for an app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps")

	// On an unclaimed device manager, there will be no AccessLists.
	deviceStub := device.DeviceClient("dm/device")
	if _, _, err := deviceStub.GetPermissions(selfCtx); err == nil {
		t.Fatalf("GetPermissions should have failed but didn't.")
	}

	// Claim the devicemanager as "root/self/mydevice"
	claimDevice(t, selfCtx, "dm", "mydevice", noPairingToken)
	expectedAccessList := make(access.Permissions)
	for _, tag := range access.AllTypicalTags() {
		expectedAccessList[string(tag)] = access.AccessList{In: []security.BlessingPattern{"root/$", "root/self/$", "root/self/mydevice/$"}}
	}
	var b bytes.Buffer
	if err := expectedAccessList.WriteTo(&b); err != nil {
		t.Fatalf("Failed to save AccessList:%v", err)
	}
	md5hash := md5.Sum(b.Bytes())
	expectedETAG := hex.EncodeToString(md5hash[:])
	acl, etag, err := deviceStub.GetPermissions(selfCtx)
	if err != nil {
		t.Fatal(err)
	}
	if etag != expectedETAG {
		t.Fatalf("getAccessList expected:%v(%v), got:%v(%v)", expectedAccessList, expectedETAG, acl, etag)
	}
	// Install from octx should fail, since it does not match the AccessList.
	installAppExpectError(t, octx, verror.ErrNoAccess.ID)

	newAccessList := make(access.Permissions)
	for _, tag := range access.AllTypicalTags() {
		newAccessList.Add("root/other", string(tag))
	}
	if err := deviceStub.SetPermissions(selfCtx, newAccessList, "invalid"); err == nil {
		t.Fatalf("SetPermissions should have failed with invalid etag")
	}
	if err := deviceStub.SetPermissions(selfCtx, newAccessList, etag); err != nil {
		t.Fatal(err)
	}
	// Install should now fail with selfCtx, which no longer matches the
	// AccessLists but succeed with octx, which does.
	installAppExpectError(t, selfCtx, verror.ErrNoAccess.ID)
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
	ctx, shutdown := initForTest()
	defer shutdown()

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()
	testDir, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	// No need to call SaveCreatorInfo() here because that's part of SelfInstall below

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
	if err := impl.SelfInstall(dmDir, suidHelperPath, agentPath, initHelperPath, "", singleUser, sessionMode, initMode, dmargs[1:], dmenv, os.Stderr, os.Stdout); err != nil {
		t.Fatalf("SelfInstall failed: %v", err)
	}

	resolveExpectNotFound(t, ctx, "dm")
	// Start the device manager.
	stdout := make(simpleRW, 100)
	defer os.Setenv(redirectEnv, os.Getenv(redirectEnv))
	os.Setenv(redirectEnv, "1")
	if err := impl.Start(dmDir, os.Stderr, stdout); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	dms := expect.NewSession(t, stdout, mgmttest.ExpectTimeout)
	mgmttest.ReadPID(t, dms)
	claimDevice(t, ctx, "dm", "mydevice", noPairingToken)
	revertDeviceExpectError(t, ctx, "dm", impl.ErrUpdateNoOp.ID) // No previous version available.

	// Stop the device manager.
	if err := impl.Stop(ctx, dmDir, os.Stderr, os.Stdout); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	dms.Expect("dm terminated")

	// Uninstall.
	if err := impl.Uninstall(dmDir, suidHelperPath, os.Stderr, os.Stdout); err != nil {
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
	ctx, shutdown := initForTest()
	defer shutdown()

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Device must be claimed before applications can be installed.
	claimDevice(t, ctx, "dm", "mydevice", noPairingToken)
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

	// Base name of argv[0] that the app should have when it executes
	// It will be path.Base(envelope.Title + "@" + envelope.Binary.File + "/app").
	// Note the suffix, which ensures that the result is always "app" at the moment.
	// Someday in future we may remove that and have binary names that reflect the app name.
	const appName = "app"

	testcases := []globTestVector{
		{"dm", "...", []string{
			"",
			"apps",
			"apps/google naps",
			"apps/google naps/" + install1ID,
			"apps/google naps/" + install1ID + "/" + instance1ID,
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs/STDERR-<timestamp>",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs/STDOUT-<timestamp>",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs/" + appName + ".INFO",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/logs/" + appName + ".<*>.INFO.<timestamp>",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/pprof",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/stats",
			"apps/google naps/" + install1ID + "/" + instance1ID + "/stats/rpc",
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
			appName + ".INFO",
			appName + ".<*>.INFO.<timestamp>",
		}},
		{"dm/apps/google naps/" + install1ID + "/" + instance1ID + "/stats/system", "start-time*", []string{"start-time-rfc1123", "start-time-unix"}},
	}

	res := newGlobTestRegexHelper(appName)

	verifyGlob(t, ctx, appName, testcases, res)
	verifyLog(t, ctx, "dm", "apps/google naps", install1ID, instance1ID, "logs", "*")
	verifyStatsValues(t, ctx, "dm", "apps/google naps", install1ID, instance1ID, "stats/system/start-time*")
	verifyPProfCmdLine(t, ctx, appName, "dm", "apps/google naps", install1ID, instance1ID, "pprof")
}

// TODO(caprita): We need better test coverage for how updating/reverting apps
// affects the package configured for the app.
func TestDeviceManagerPackages(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	binaryVON := "realbin"
	defer startRealBinaryRepository(t, ctx, binaryVON)()

	// upload package to binary repository
	tmpdir, err := ioutil.TempDir("", "test-package-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)
	createFile := func(name, contents string) {
		if err := ioutil.WriteFile(filepath.Join(tmpdir, name), []byte(contents), 0600); err != nil {
			t.Fatalf("ioutil.WriteFile failed: %v", err)
		}
	}
	createFile("hello.txt", "Hello World!")
	if _, err := libbinary.UploadFromDir(ctx, naming.Join(binaryVON, "testpkg"), tmpdir); err != nil {
		t.Fatalf("libbinary.UploadFromDir failed: %v", err)
	}
	createAndUpload := func(von, contents string) {
		createFile("tempfile", contents)
		if _, err := libbinary.UploadFromFile(ctx, naming.Join(binaryVON, von), filepath.Join(tmpdir, "tempfile")); err != nil {
			t.Fatalf("libbinary.UploadFromFile failed: %v", err)
		}
	}
	createAndUpload("testfile", "Goodbye World!")
	createAndUpload("leftshark", "Left shark")
	createAndUpload("rightshark", "Right shark")
	createAndUpload("beachball", "Beach ball")

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")
	envelope.Packages = map[string]application.SignedFile{
		"test": application.SignedFile{
			File: "realbin/testpkg",
		},
		"test2": application.SignedFile{
			File: "realbin/testfile",
		},
		"shark": application.SignedFile{
			File: "realbin/leftshark",
		},
	}

	// These are install-time overrides for packages.
	// Specifically, we override the 'shark' package and add a new
	// 'ball' package on top of what's specified in the envelope.
	packages := application.Packages{
		"shark": application.SignedFile{
			File: "realbin/rightshark",
		},
		"ball": application.SignedFile{
			File: "realbin/beachball",
		},
	}
	// Device must be claimed before apps can be installed.
	claimDevice(t, ctx, "dm", "mydevice", noPairingToken)
	// Install the app.
	appID := installApp(t, ctx, packages)

	// Start an instance of the app.
	startApp(t, ctx, appID)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}

	for _, c := range []struct {
		path, content string
	}{
		{
			filepath.Join("test", "hello.txt"),
			"Hello World!",
		},
		{
			"test2",
			"Goodbye World!",
		},
		{
			"shark",
			"Right shark",
		},
		{
			"ball",
			"Beach ball",
		},
	} {
		// Ask the app to cat the file.
		file := filepath.Join("packages", c.path)
		name := "appV1"
		content, err := cat(ctx, name, file)
		if err != nil {
			t.Errorf("cat(%q, %q) failed: %v", name, file, err)
		}
		if expected := c.content; content != expected {
			t.Errorf("unexpected content: expected %q, got %q", expected, content)
		}
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
	ctx, shutdown := initForTest()
	defer shutdown()

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// By default, the two processes (selfCtx and octx) will have blessings
	// generated based on the username/machine name running this process.
	// Since these blessings will appear in AccessLists, give them
	// recognizable names.
	idp := testutil.NewIDProvider("root")
	selfCtx := ctxWithNewPrincipal(t, ctx, idp, "self")
	otherCtx := ctxWithNewPrincipal(t, selfCtx, idp, "other")
	// Both the "external" processes must recognize the root mounttable's
	// blessings, otherwise they will not talk to it.
	for _, c := range []*context.T{selfCtx, otherCtx} {
		v23.GetPrincipal(c).AddToRoots(v23.GetPrincipal(ctx).BlessingStore().Default())
	}

	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)

	deviceStub := device.DeviceClient("dm/device")
	// Attempt to list associations on the device manager without having
	// claimed it.
	if list, err := deviceStub.ListAssociations(otherCtx); err == nil {
		t.Fatalf("ListAssociations should fail on unclaimed device manager but did not: (%v, %v)", list, err)
	}

	// self claims the device manager.
	claimDevice(t, selfCtx, "dm", "alice", noPairingToken)

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
	ctx, shutdown := initForTest()
	defer shutdown()

	// Identity provider used to ensure that all processes recognize each
	// others' blessings.
	idp := testutil.NewIDProvider("root")
	if err := idp.Bless(v23.GetPrincipal(ctx), "self"); err != nil {
		t.Fatal(err)
	}

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	selfCtx := ctx
	otherCtx := ctxWithNewPrincipal(t, selfCtx, idp, "other")

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "-mocksetuid", "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)
	// Claim the devicemanager with selfCtx as root/self/alice
	claimDevice(t, selfCtx, "dm", "alice", noPairingToken)

	deviceStub := device.DeviceClient("dm/device")

	// Create the local server that the app uses to tell us which system
	// name the device manager wished to run it as.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, []string{testEnvVarName + "=env-var"}, appCmd, "google naps", fmt.Sprintf("--%s=flag-val-envelope", testFlagName), "appV1")

	// Install and start the app as root/self.
	appID := installApp(t, selfCtx)

	// Start an instance of the app but this time it should fail: we do not
	// have an associated uname for the invoking identity.
	startAppExpectError(t, selfCtx, appID, verror.ErrNoAccess.ID)

	// Create an association for selfCtx
	if err := deviceStub.AssociateAccount(selfCtx, []string{"root/self"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	instance1ID := startApp(t, selfCtx, appID)
	verifyPingArgs(t, pingCh, testUserName, "flag-val-envelope", "env-var") // Wait until the app pings us that it's ready.
	stopApp(t, selfCtx, appID, instance1ID)

	vlog.VI(2).Infof("other attempting to run an app without access. Should fail.")
	startAppExpectError(t, otherCtx, appID, verror.ErrNoAccess.ID)

	// Self will now let other also install apps.
	if err := deviceStub.AssociateAccount(selfCtx, []string{"root/other"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	// Add Start to the AccessList list for root/other.
	newAccessList, _, err := deviceStub.GetPermissions(selfCtx)
	if err != nil {
		t.Fatalf("GetPermissions failed %v", err)
	}
	newAccessList.Add("root/other", string(access.Write))
	if err := deviceStub.SetPermissions(selfCtx, newAccessList, ""); err != nil {
		t.Fatalf("SetPermissions failed %v", err)
	}

	// With the introduction of per installation and per instance AccessLists,
	// while other now has administrator permissions on the device manager,
	// other doesn't have execution permissions for the app. So this will
	// fail.
	vlog.VI(2).Infof("other attempting to run an app still without access. Should fail.")
	startAppExpectError(t, otherCtx, appID, verror.ErrNoAccess.ID)

	// But self can give other permissions  to start applications.
	vlog.VI(2).Infof("self attempting to give other permission to start %s", appID)
	newAccessList, _, err = appStub(appID).GetPermissions(selfCtx)
	if err != nil {
		t.Fatalf("GetPermissions on appID: %v failed %v", appID, err)
	}
	newAccessList.Add("root/other", string(access.Read))
	if err = appStub(appID).SetPermissions(selfCtx, newAccessList, ""); err != nil {
		t.Fatalf("SetPermissions on appID: %v failed: %v", appID, err)
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
	resumeAppExpectError(t, otherCtx, appID, instance2ID, verror.ErrNoAccess.ID)

	// Clean up.
	stopApp(t, otherCtx, appID, instance2ID)

	vlog.VI(2).Infof("Show that Start with different systemName works.")
	instance3ID := startApp(t, otherCtx, appID)
	verifyPingArgs(t, pingCh, anotherTestUserName, "flag-val-envelope", "env-var") // Wait until the app pings us that it's ready.

	// Clean up.
	stopApp(t, otherCtx, appID, instance3ID)
}

func TestDownloadSignatureMatch(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	binaryVON := "binary"
	pkgVON := naming.Join(binaryVON, "testpkg")
	defer startRealBinaryRepository(t, ctx, binaryVON)()

	up := testutil.RandomBytes(testutil.Intn(5 << 20))
	mediaInfo := repository.MediaInfo{Type: "application/octet-stream"}
	sig, err := libbinary.Upload(ctx, naming.Join(binaryVON, "testbinary"), up, mediaInfo)
	if err != nil {
		t.Fatalf("Upload(%v) failed:%v", binaryVON, err)
	}

	// Upload packages for this application
	tmpdir, err := ioutil.TempDir("", "test-package-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)
	pkgContents := testutil.RandomBytes(testutil.Intn(5 << 20))
	if err := ioutil.WriteFile(filepath.Join(tmpdir, "pkg.txt"), pkgContents, 0600); err != nil {
		t.Fatalf("ioutil.WriteFile failed: %v", err)
	}
	pkgSig, err := libbinary.UploadFromDir(ctx, pkgVON, tmpdir)
	if err != nil {
		t.Fatalf("libbinary.UploadFromDir failed: %v", err)
	}

	// Start the application repository
	envelope, serverStop := startApplicationRepository(ctx)
	defer serverStop()

	root, cleanup := mgmttest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := mgmttest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)
	claimDevice(t, ctx, "dm", "mydevice", noPairingToken)

	publisher, err := v23.GetPrincipal(ctx).BlessSelf("publisher")
	if err != nil {
		t.Fatalf("Failed to generate publisher blessings:%v", err)
	}
	*envelope = application.Envelope{
		Binary: application.SignedFile{
			File:      naming.Join(binaryVON, "testbinary"),
			Signature: *sig,
		},
		Publisher: publisher,
		Packages: map[string]application.SignedFile{
			"pkg": application.SignedFile{
				File:      pkgVON,
				Signature: *pkgSig,
			},
		},
	}
	if _, err := appStub().Install(ctx, mockApplicationRepoName, device.Config{}, nil); err != nil {
		t.Fatalf("Failed to Install app:%v", err)
	}

	// Verify that when the binary is corrupted, signature verification fails.
	up[0] = up[0] ^ 0xFF
	if err := libbinary.Delete(ctx, naming.Join(binaryVON, "testbinary")); err != nil {
		t.Fatalf("Delete(%v) failed:%v", binaryVON, err)
	}
	if _, err := libbinary.Upload(ctx, naming.Join(binaryVON, "testbinary"), up, mediaInfo); err != nil {
		t.Fatalf("Upload(%v) failed:%v", binaryVON, err)
	}
	if _, err := appStub().Install(ctx, mockApplicationRepoName, device.Config{}, nil); verror.ErrorID(err) != impl.ErrOperationFailed.ID {
		t.Fatalf("Failed to verify signature mismatch for binary:%v. Got errorid=%v[%v], want errorid=%v", binaryVON, verror.ErrorID(err), err, impl.ErrOperationFailed.ID)
	}

	// Restore the binary and verify that installation succeeds.
	up[0] = up[0] ^ 0xFF
	if err := libbinary.Delete(ctx, naming.Join(binaryVON, "testbinary")); err != nil {
		t.Fatalf("Delete(%v) failed:%v", binaryVON, err)
	}
	if _, err := libbinary.Upload(ctx, naming.Join(binaryVON, "testbinary"), up, mediaInfo); err != nil {
		t.Fatalf("Upload(%v) failed:%v", binaryVON, err)
	}
	if _, err := appStub().Install(ctx, mockApplicationRepoName, device.Config{}, nil); err != nil {
		t.Fatalf("Failed to Install app:%v", err)
	}

	// Verify that when the package contents are corrupted, signature verification fails.
	pkgContents[0] = pkgContents[0] ^ 0xFF
	if err := libbinary.Delete(ctx, pkgVON); err != nil {
		t.Fatalf("Delete(%v) failed:%v", pkgVON, err)
	}
	if err := os.Remove(filepath.Join(tmpdir, "pkg.txt")); err != nil {
		t.Fatalf("Remove(%v) failed:%v", filepath.Join(tmpdir, "pkg.txt"), err)
	}
	if err := ioutil.WriteFile(filepath.Join(tmpdir, "pkg.txt"), pkgContents, 0600); err != nil {
		t.Fatalf("ioutil.WriteFile failed: %v", err)
	}
	if _, err = libbinary.UploadFromDir(ctx, pkgVON, tmpdir); err != nil {
		t.Fatalf("libbinary.UploadFromDir failed: %v", err)
	}
	if _, err := appStub().Install(ctx, mockApplicationRepoName, device.Config{}, nil); verror.ErrorID(err) != impl.ErrOperationFailed.ID {
		t.Fatalf("Failed to verify signature mismatch for package:%v", pkgVON)
	}
}
