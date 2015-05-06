// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO(caprita): This file is becoming unmanageable; split into several test
// files.
// TODO(rjkroege): Add a more extensive unit test case to exercise AccessList logic.

package impl_test

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/x/lib/vlog"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/application"
	"v.io/v23/services/device"

	"v.io/x/ref/envvar"
	"v.io/x/ref/lib/mgmt"
	"v.io/x/ref/services/device/internal/config"
	"v.io/x/ref/services/device/internal/impl"
	"v.io/x/ref/services/device/internal/impl/utiltest"
	"v.io/x/ref/services/internal/binarylib"
	"v.io/x/ref/services/internal/servicetest"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/testutil"
)

func TestMain(m *testing.M) {
	utiltest.TestMainImpl(m)
}

// TestSuidHelper is testing boilerplate for suidhelper that does not
// create a runtime because the suidhelper is not a Vanadium application.
func TestSuidHelper(t *testing.T) {
	utiltest.TestSuidHelperImpl(t)
}

// TODO(rjkroege): generateDeviceManagerScript and generateSuidHelperScript have
// code similarity that might benefit from refactoring.
// generateDeviceManagerScript is very similar in behavior to generateScript in
// device_invoker.go.  However, we chose to re-implement it here for two
// reasons: (1) avoid making generateScript public; and (2) how the test choses
// to invoke the device manager subprocess the first time should be independent
// of how device manager implementation sets up its updated versions.
func generateDeviceManagerScript(t *testing.T, root string, args, env []string) string {
	env = impl.VanadiumEnvironment(env)
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
// supposed to. The initial device manager is running 'by hand' via a module
// command. Further versions are running through the soft link that the device
// manager itself updates.
func TestDeviceManagerUpdateAndRevert(t *testing.T) {
	ctx, shutdown := utiltest.InitForTest()
	defer shutdown()

	sh, deferFn := servicetest.CreateShellAndMountTable(t, ctx, v23.GetPrincipal(ctx))
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := utiltest.StartMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := servicetest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Current link does not have to live in the root dir, but it's
	// convenient to put it there so we have everything in one place.
	currLink := filepath.Join(root, "current_link")

	// Since the device manager will be restarting, use the
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
	dmEnv := []string{fmt.Sprintf("%v=%v", envvar.Credentials, dmCreds)}
	dmArgs := []string{"factoryDM", root, "unused_helper", utiltest.MockApplicationRepoName, currLink}
	args, env := sh.CommandEnvelope(utiltest.DeviceManagerCmd, dmEnv, dmArgs...)
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
	// demonstrates that the initial device manager could be running by hand
	// as long as the right initial configuration is passed into the device
	// manager implementation.
	dmh := servicetest.RunCommand(t, sh, dmPauseBeforeStopEnv, utiltest.DeviceManagerCmd, dmArgs...)
	defer func() {
		syscall.Kill(dmh.Pid(), syscall.SIGINT)
		utiltest.VerifyNoRunningProcesses(t)
	}()

	servicetest.ReadPID(t, dmh)
	utiltest.Resolve(t, ctx, "claimable", 1)
	// Brand new device manager must be claimed first.
	utiltest.ClaimDevice(t, ctx, "claimable", "factoryDM", "mydevice", utiltest.NoPairingToken)
	// Simulate an invalid envelope in the application repository.
	*envelope = utiltest.EnvelopeFromShell(sh, dmPauseBeforeStopEnv, utiltest.DeviceManagerCmd, "bogus", dmArgs...)

	utiltest.UpdateDeviceExpectError(t, ctx, "factoryDM", impl.ErrAppTitleMismatch.ID)
	utiltest.RevertDeviceExpectError(t, ctx, "factoryDM", impl.ErrUpdateNoOp.ID)

	// Set up a second version of the device manager. The information in the
	// envelope will be used by the device manager to stage the next
	// version.
	*envelope = utiltest.EnvelopeFromShell(sh, dmEnv, utiltest.DeviceManagerCmd, application.DeviceManagerTitle, "v2DM")
	utiltest.UpdateDevice(t, ctx, "factoryDM")

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

	utiltest.UpdateDeviceExpectError(t, ctx, "factoryDM", impl.ErrOperationInProgress.ID)

	dmh.CloseStdin()

	dmh.Expect("restart handler")
	dmh.Expect("factoryDM terminated")
	dmh.Shutdown(os.Stderr, os.Stderr)

	// A successful update means the device manager has stopped itself.  We
	// relaunch it from the current link.
	utiltest.ResolveExpectNotFound(t, ctx, "v2DM") // Ensure a clean slate.

	dmh = servicetest.RunCommand(t, sh, dmEnv, utiltest.ExecScriptCmd, currLink)

	servicetest.ReadPID(t, dmh)
	utiltest.Resolve(t, ctx, "v2DM", 1) // Current link should have been launching v2.

	// Try issuing an update without changing the envelope in the
	// application repository: this should fail, and current link should be
	// unchanged.
	utiltest.UpdateDeviceExpectError(t, ctx, "v2DM", impl.ErrUpdateNoOp.ID)
	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Try issuing an update with a binary that has a different major version
	// number. It should fail.
	utiltest.ResolveExpectNotFound(t, ctx, "v2.5DM") // Ensure a clean slate.
	*envelope = utiltest.EnvelopeFromShell(sh, dmEnv, utiltest.DeviceManagerV10Cmd, application.DeviceManagerTitle, "v2.5DM")
	utiltest.UpdateDeviceExpectError(t, ctx, "v2DM", impl.ErrOperationFailed.ID)

	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Create a third version of the device manager and issue an update.
	*envelope = utiltest.EnvelopeFromShell(sh, dmEnv, utiltest.DeviceManagerCmd, application.DeviceManagerTitle, "v3DM")
	utiltest.UpdateDevice(t, ctx, "v2DM")

	scriptPathV3 := evalLink()
	if scriptPathV3 == scriptPathV2 {
		t.Fatalf("current link didn't change")
	}

	dmh.Expect("restart handler")
	dmh.Expect("v2DM terminated")

	dmh.Shutdown(os.Stderr, os.Stderr)

	utiltest.ResolveExpectNotFound(t, ctx, "v3DM") // Ensure a clean slate.

	// Re-lanuch the device manager from current link.  We instruct the
	// device manager to pause before stopping its server, so that we can
	// verify that a second revert fails while a revert is in progress.
	dmh = servicetest.RunCommand(t, sh, dmPauseBeforeStopEnv, utiltest.ExecScriptCmd, currLink)

	servicetest.ReadPID(t, dmh)
	utiltest.Resolve(t, ctx, "v3DM", 1) // Current link should have been launching v3.

	// Revert the device manager to its previous version (v2).
	utiltest.RevertDevice(t, ctx, "v3DM")
	utiltest.RevertDeviceExpectError(t, ctx, "v3DM", impl.ErrOperationInProgress.ID) // Revert already in progress.
	dmh.CloseStdin()
	dmh.Expect("restart handler")
	dmh.Expect("v3DM terminated")
	if evalLink() != scriptPathV2 {
		t.Fatalf("current link was not reverted correctly")
	}
	dmh.Shutdown(os.Stderr, os.Stderr)

	utiltest.ResolveExpectNotFound(t, ctx, "v2DM") // Ensure a clean slate.

	dmh = servicetest.RunCommand(t, sh, dmEnv, utiltest.ExecScriptCmd, currLink)
	servicetest.ReadPID(t, dmh)
	utiltest.Resolve(t, ctx, "v2DM", 1) // Current link should have been launching v2.

	// Revert the device manager to its previous version (factory).
	utiltest.RevertDevice(t, ctx, "v2DM")
	dmh.Expect("restart handler")
	dmh.Expect("v2DM terminated")
	if evalLink() != scriptPathFactory {
		t.Fatalf("current link was not reverted correctly")
	}
	dmh.Shutdown(os.Stderr, os.Stderr)

	utiltest.ResolveExpectNotFound(t, ctx, "factoryDM") // Ensure a clean slate.

	dmh = servicetest.RunCommand(t, sh, dmEnv, utiltest.ExecScriptCmd, currLink)
	servicetest.ReadPID(t, dmh)
	utiltest.Resolve(t, ctx, "factoryDM", 1) // Current link should have been launching factory version.
	utiltest.ShutdownDevice(t, ctx, "factoryDM")
	dmh.Expect("factoryDM terminated")
	dmh.ExpectEOF()

	// Re-launch the device manager, to exercise the behavior of Stop.
	utiltest.ResolveExpectNotFound(t, ctx, "factoryDM") // Ensure a clean slate.
	dmh = servicetest.RunCommand(t, sh, dmEnv, utiltest.ExecScriptCmd, currLink)
	servicetest.ReadPID(t, dmh)
	utiltest.Resolve(t, ctx, "factoryDM", 1)
	utiltest.KillDevice(t, ctx, "factoryDM")
	dmh.Expect("restart handler")
	dmh.Expect("factoryDM terminated")
	dmh.ExpectEOF()
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

// TestLifeOfAnApp installs an app, instantiates, runs, kills, and deletes
// several instances, and performs updates.
func TestLifeOfAnApp(t *testing.T) {
	ctx, shutdown := utiltest.InitForTest()
	defer shutdown()

	sh, deferFn := servicetest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := utiltest.StartMockRepos(t, ctx)
	defer cleanup()

	root, cleanup := servicetest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := utiltest.GenerateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh := servicetest.RunCommand(t, sh, nil, utiltest.DeviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	servicetest.ReadPID(t, dmh)
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", utiltest.NoPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := utiltest.SetupPingServer(t, ctx)
	defer cleanup()

	utiltest.Resolve(t, ctx, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = utiltest.EnvelopeFromShell(sh, []string{utiltest.TestEnvVarName + "=env-val-envelope"}, utiltest.AppCmd, "google naps", fmt.Sprintf("--%s=flag-val-envelope", utiltest.TestFlagName), "appV1")

	// Install the app.  The config-specified flag value for testFlagName
	// should override the value specified in the envelope above, and the
	// config-specified value for origin should override the value in the
	// Install rpc argument.
	mtName, ok := sh.GetVar(envvar.NamespacePrefix)
	if !ok {
		t.Fatalf("failed to get namespace root var from shell")
	}
	// This rooted name should be equivalent to the relative name "ar", but
	// we want to test that the config override for origin works.
	rootedAppRepoName := naming.Join(mtName, "ar")
	appID := utiltest.InstallApp(t, ctx, device.Config{utiltest.TestFlagName: "flag-val-install", mgmt.AppOriginConfigKey: rootedAppRepoName})
	v1 := utiltest.VerifyState(t, ctx, device.InstallationStateActive, appID)
	installationDebug := utiltest.Debug(t, ctx, appID)
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
	if _, err := utiltest.LaunchAppImpl(t, ctx, appID, ""); err == nil || err.Error() != expectedErr {
		t.Fatalf("Start(%v) expected to fail with %v, got %v instead", appID, expectedErr, err)
	}

	// Start an instance of the app.
	instance1ID := utiltest.LaunchApp(t, ctx, appID)
	if v := utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance1ID); v != v1 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v1, v)
	}

	instanceDebug := utiltest.Debug(t, ctx, appID, instance1ID)
	// Verify the apps default blessings.
	if !strings.Contains(instanceDebug, fmt.Sprintf("Default Blessings                %s/forapp", test.TestBlessing)) {
		t.Fatalf("debug response doesn't contain expected info: %v", instanceDebug)
	}

	// Wait until the app pings us that it's ready.
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "flag-val-install", "env-val-envelope")

	v1EP1 := utiltest.Resolve(t, ctx, "appV1", 1)[0]

	// Stop the app instance.
	utiltest.KillApp(t, ctx, appID, instance1ID)
	utiltest.VerifyState(t, ctx, device.InstanceStateNotRunning, appID, instance1ID)
	utiltest.ResolveExpectNotFound(t, ctx, "appV1")

	utiltest.RunApp(t, ctx, appID, instance1ID)
	utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance1ID)
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.
	oldV1EP1 := v1EP1
	if v1EP1 = utiltest.Resolve(t, ctx, "appV1", 1)[0]; v1EP1 == oldV1EP1 {
		t.Fatalf("Expected a new endpoint for the app after kill/run")
	}

	// Start a second instance.
	instance2ID := utiltest.LaunchApp(t, ctx, appID)
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.

	// There should be two endpoints mounted as "appV1", one for each
	// instance of the app.
	endpoints := utiltest.Resolve(t, ctx, "appV1", 2)
	v1EP2 := endpoints[0]
	if endpoints[0] == v1EP1 {
		v1EP2 = endpoints[1]
		if v1EP2 == v1EP1 {
			t.Fatalf("Both endpoints are the same")
		}
	} else if endpoints[1] != v1EP1 {
		t.Fatalf("Second endpoint should have been v1EP1: %v, %v", endpoints, v1EP1)
	}

	// TODO(caprita): verify various non-standard combinations (kill when
	// canceled; run while still running).

	// Kill the first instance.
	utiltest.KillApp(t, ctx, appID, instance1ID)
	// Only the second instance should still be running and mounted.
	if want, got := v1EP2, utiltest.Resolve(t, ctx, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Updating the installation to itself is a no-op.
	utiltest.UpdateAppExpectError(t, ctx, appID, impl.ErrUpdateNoOp.ID)

	// Updating the installation should not work with a mismatched title.
	*envelope = utiltest.EnvelopeFromShell(sh, nil, utiltest.AppCmd, "bogus")

	utiltest.UpdateAppExpectError(t, ctx, appID, impl.ErrAppTitleMismatch.ID)

	// Create a second version of the app and update the app to it.
	*envelope = utiltest.EnvelopeFromShell(sh, []string{utiltest.TestEnvVarName + "=env-val-envelope"}, utiltest.AppCmd, "google naps", "appV2")

	utiltest.UpdateApp(t, ctx, appID)

	v2 := utiltest.VerifyState(t, ctx, device.InstallationStateActive, appID)
	if v1 == v2 {
		t.Fatalf("Version did not change for %v: %v", appID, v1)
	}

	// Second instance should still be running.
	if want, got := v1EP2, utiltest.Resolve(t, ctx, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}
	if v := utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance2ID); v != v1 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v1, v)
	}

	// Resume first instance.
	utiltest.RunApp(t, ctx, appID, instance1ID)
	if v := utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance1ID); v != v1 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v1, v)
	}
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.
	// Both instances should still be running the first version of the app.
	// Check that the mounttable contains two endpoints, one of which is
	// v1EP2.
	endpoints = utiltest.Resolve(t, ctx, "appV1", 2)
	if endpoints[0] == v1EP2 {
		if endpoints[1] == v1EP2 {
			t.Fatalf("Both endpoints are the same")
		}
	} else if endpoints[1] != v1EP2 {
		t.Fatalf("Second endpoint should have been v1EP2: %v, %v", endpoints, v1EP2)
	}

	// Trying to update first instance while it's running should fail.
	utiltest.UpdateInstanceExpectError(t, ctx, appID, instance1ID, impl.ErrInvalidOperation.ID)
	// Stop first instance and try again.
	utiltest.KillApp(t, ctx, appID, instance1ID)
	// Only the second instance should still be running and mounted.
	if want, got := v1EP2, utiltest.Resolve(t, ctx, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}
	// Update succeeds now.
	utiltest.UpdateInstance(t, ctx, appID, instance1ID)
	if v := utiltest.VerifyState(t, ctx, device.InstanceStateNotRunning, appID, instance1ID); v != v2 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v2, v)
	}
	// Resume the first instance and verify it's running v2 now.
	utiltest.RunApp(t, ctx, appID, instance1ID)
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "flag-val-install", "env-val-envelope")
	utiltest.Resolve(t, ctx, "appV1", 1)
	utiltest.Resolve(t, ctx, "appV2", 1)

	// Stop first instance.
	utiltest.TerminateApp(t, ctx, appID, instance1ID)
	verifyAppWorkspace(t, root, appID, instance1ID)
	utiltest.ResolveExpectNotFound(t, ctx, "appV2")

	// Start a third instance.
	instance3ID := utiltest.LaunchApp(t, ctx, appID)
	if v := utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance3ID); v != v2 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v2, v)
	}
	// Wait until the app pings us that it's ready.
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "flag-val-install", "env-val-envelope")

	utiltest.Resolve(t, ctx, "appV2", 1)

	// Stop second instance.
	utiltest.TerminateApp(t, ctx, appID, instance2ID)
	utiltest.ResolveExpectNotFound(t, ctx, "appV1")

	// Stop third instance.
	utiltest.TerminateApp(t, ctx, appID, instance3ID)
	utiltest.ResolveExpectNotFound(t, ctx, "appV2")

	// Revert the app.
	utiltest.RevertApp(t, ctx, appID)
	if v := utiltest.VerifyState(t, ctx, device.InstallationStateActive, appID); v != v1 {
		t.Fatalf("Installation version expected to be %v, got %v instead", v1, v)
	}

	// Start a fourth instance.  It should be running from version 1.
	instance4ID := utiltest.LaunchApp(t, ctx, appID)
	if v := utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance4ID); v != v1 {
		t.Fatalf("Instance version expected to be %v, got %v instead", v1, v)
	}
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "flag-val-install", "env-val-envelope") // Wait until the app pings us that it's ready.
	utiltest.Resolve(t, ctx, "appV1", 1)
	utiltest.TerminateApp(t, ctx, appID, instance4ID)
	utiltest.ResolveExpectNotFound(t, ctx, "appV1")

	// We are already on the first version, no further revert possible.
	utiltest.RevertAppExpectError(t, ctx, appID, impl.ErrUpdateNoOp.ID)

	// Uninstall the app.
	utiltest.UninstallApp(t, ctx, appID)
	utiltest.VerifyState(t, ctx, device.InstallationStateUninstalled, appID)

	// Updating the installation should no longer be allowed.
	utiltest.UpdateAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Reverting the installation should no longer be allowed.
	utiltest.RevertAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Starting new instances should no longer be allowed.
	utiltest.LaunchAppExpectError(t, ctx, appID, impl.ErrInvalidOperation.ID)

	// Make sure that Kill will actually kill an app that doesn't exit
	// cleanly Do this by installing, instantiating, running, and killing
	// hangingApp, which sleeps (rather than exits) after being asked to
	// Stop()
	*envelope = utiltest.EnvelopeFromShell(sh, nil, utiltest.HangingAppCmd, "hanging ap", "hAppV1")
	hAppID := utiltest.InstallApp(t, ctx)
	hInstanceID := utiltest.LaunchApp(t, ctx, hAppID)
	hangingPid := pingCh.WaitForPingArgs(t).Pid
	if err := syscall.Kill(hangingPid, 0); err != nil && err != syscall.EPERM {
		t.Fatalf("Pid of hanging app (%v) is not live", hangingPid)
	}
	utiltest.KillApp(t, ctx, hAppID, hInstanceID)
	pidIsAlive := true
	for i := 0; i < 10 && pidIsAlive; i++ {
		if err := syscall.Kill(hangingPid, 0); err == nil || err == syscall.EPERM {
			time.Sleep(time.Second) // pid is still alive
		} else {
			pidIsAlive = false
		}
	}
	if pidIsAlive {
		t.Fatalf("Pid of hanging app (%d) has not exited after Stop() call", hangingPid)
	}

	// Cleanly shut down the device manager.
	defer utiltest.VerifyNoRunningProcesses(t)
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
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
	ctx, shutdown := utiltest.InitForTest()
	defer shutdown()

	sh, deferFn := servicetest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()
	testDir, cleanup := servicetest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	// No need to call SaveCreatorInfo() here because that's part of SelfInstall below

	// Create a script wrapping the test target that implements suidhelper.
	suidHelperPath := utiltest.GenerateSuidHelperScript(t, testDir)
	// Create a dummy script mascarading as the security agent.
	agentPath := utiltest.GenerateAgentScript(t, testDir)
	initHelperPath := ""

	// Create an 'envelope' for the device manager that we can pass to the
	// installer, to ensure that the device manager that the installer
	// configures can run.
	dmargs, dmenv := sh.CommandEnvelope(utiltest.DeviceManagerCmd, nil, "dm")
	dmDir := filepath.Join(testDir, "dm")
	// TODO(caprita): Add test logic when initMode = true.
	singleUser, sessionMode, initMode := true, true, false
	if err := impl.SelfInstall(dmDir, suidHelperPath, agentPath, initHelperPath, "", singleUser, sessionMode, initMode, dmargs[1:], dmenv, os.Stderr, os.Stdout); err != nil {
		t.Fatalf("SelfInstall failed: %v", err)
	}

	utiltest.ResolveExpectNotFound(t, ctx, "dm")
	// Start the device manager.
	stdout := make(simpleRW, 100)
	defer os.Setenv(utiltest.RedirectEnv, os.Getenv(utiltest.RedirectEnv))
	os.Setenv(utiltest.RedirectEnv, "1")
	if err := impl.Start(dmDir, os.Stderr, stdout); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	dms := expect.NewSession(t, stdout, servicetest.ExpectTimeout)
	servicetest.ReadPID(t, dms)
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", utiltest.NoPairingToken)
	utiltest.RevertDeviceExpectError(t, ctx, "dm", impl.ErrUpdateNoOp.ID) // No previous version available.

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

// TODO(caprita): We need better test coverage for how updating/reverting apps
// affects the package configured for the app.
func TestDeviceManagerPackages(t *testing.T) {
	ctx, shutdown := utiltest.InitForTest()
	defer shutdown()

	sh, deferFn := servicetest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := utiltest.StartMockRepos(t, ctx)
	defer cleanup()

	binaryVON := "realbin"
	defer utiltest.StartRealBinaryRepository(t, ctx, binaryVON)()

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
	if _, err := binarylib.UploadFromDir(ctx, naming.Join(binaryVON, "testpkg"), tmpdir); err != nil {
		t.Fatalf("binarylib.UploadFromDir failed: %v", err)
	}
	createAndUpload := func(von, contents string) {
		createFile("tempfile", contents)
		if _, err := binarylib.UploadFromFile(ctx, naming.Join(binaryVON, von), filepath.Join(tmpdir, "tempfile")); err != nil {
			t.Fatalf("binarylib.UploadFromFile failed: %v", err)
		}
	}
	createAndUpload("testfile", "Goodbye World!")
	createAndUpload("leftshark", "Left shark")
	createAndUpload("rightshark", "Right shark")
	createAndUpload("beachball", "Beach ball")

	root, cleanup := servicetest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := utiltest.GenerateSuidHelperScript(t, root)

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh := servicetest.RunCommand(t, sh, nil, utiltest.DeviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := servicetest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)
	defer utiltest.VerifyNoRunningProcesses(t)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := utiltest.SetupPingServer(t, ctx)
	defer cleanup()

	// Create the envelope for the first version of the app.
	*envelope = utiltest.EnvelopeFromShell(sh, nil, utiltest.AppCmd, "google naps", "appV1")
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
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", utiltest.NoPairingToken)
	// Install the app.
	appID := utiltest.InstallApp(t, ctx, packages)

	// Start an instance of the app.
	instance1ID := utiltest.LaunchApp(t, ctx, appID)
	defer utiltest.TerminateApp(t, ctx, appID, instance1ID)

	// Wait until the app pings us that it's ready.
	pingCh.WaitForPingArgs(t)

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
		content, err := utiltest.Cat(ctx, name, file)
		if err != nil {
			t.Errorf("utiltest.Cat(%q, %q) failed: %v", name, file, err)
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
	utiltest.CompareAssociations(t, assocs, expected)
}

// TODO(rjkroege): Verify that associations persist across restarts once
// permanent storage is added.
func TestAccountAssociation(t *testing.T) {
	ctx, shutdown := utiltest.InitForTest()
	defer shutdown()

	sh, deferFn := servicetest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	root, cleanup := servicetest.SetupRootDir(t, "devicemanager")
	defer cleanup()
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// By default, the two processes (selfCtx and octx) will have blessings
	// generated based on the username/machine name running this process.
	// Since these blessings will appear in AccessLists, give them
	// recognizable names.
	idp := testutil.NewIDProvider("root")
	selfCtx := utiltest.CtxWithNewPrincipal(t, ctx, idp, "self")
	otherCtx := utiltest.CtxWithNewPrincipal(t, selfCtx, idp, "other")
	// Both the "external" processes must recognize the root mounttable's
	// blessings, otherwise they will not talk to it.
	for _, c := range []*context.T{selfCtx, otherCtx} {
		v23.GetPrincipal(c).AddToRoots(v23.GetPrincipal(ctx).BlessingStore().Default())
	}

	dmh := servicetest.RunCommand(t, sh, nil, utiltest.DeviceManagerCmd, "dm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := servicetest.ReadPID(t, dmh)
	defer syscall.Kill(pid, syscall.SIGINT)
	defer utiltest.VerifyNoRunningProcesses(t)

	// Attempt to list associations on the device manager without having
	// claimed it.
	if list, err := device.DeviceClient("claimable").ListAssociations(otherCtx); err == nil {
		t.Fatalf("ListAssociations should fail on unclaimed device manager but did not: (%v, %v)", list, err)
	}

	// self claims the device manager.
	utiltest.ClaimDevice(t, selfCtx, "claimable", "dm", "alice", utiltest.NoPairingToken)

	vlog.VI(2).Info("Verify that associations start out empty.")
	deviceStub := device.DeviceClient("dm/device")
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
