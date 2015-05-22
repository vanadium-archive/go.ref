// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package applife_test

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/v23/naming"
	"v.io/v23/services/device"
	"v.io/x/ref"
	"v.io/x/ref/lib/mgmt"
	"v.io/x/ref/services/device/internal/impl"
	"v.io/x/ref/services/device/internal/impl/utiltest"
	"v.io/x/ref/services/internal/servicetest"
	"v.io/x/ref/test"
)

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
	dmh := servicetest.RunCommand(t, sh, nil, utiltest.DeviceManager, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	servicetest.ReadPID(t, dmh)
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", utiltest.NoPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := utiltest.SetupPingServer(t, ctx)
	defer cleanup()

	utiltest.Resolve(t, ctx, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = utiltest.EnvelopeFromShell(sh, []string{utiltest.TestEnvVarName + "=env-val-envelope"}, utiltest.App, "google naps", 0, 0, fmt.Sprintf("--%s=flag-val-envelope", utiltest.TestFlagName), "appV1")

	// Install the app.  The config-specified flag value for testFlagName
	// should override the value specified in the envelope above, and the
	// config-specified value for origin should override the value in the
	// Install rpc argument.
	mtName, ok := sh.GetVar(ref.EnvNamespacePrefix)
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
	*envelope = utiltest.EnvelopeFromShell(sh, nil, utiltest.App, "bogus", 0, 0)

	utiltest.UpdateAppExpectError(t, ctx, appID, impl.ErrAppTitleMismatch.ID)

	// Create a second version of the app and update the app to it.
	*envelope = utiltest.EnvelopeFromShell(sh, []string{utiltest.TestEnvVarName + "=env-val-envelope"}, utiltest.App, "google naps", 0, 0, "appV2")

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
	*envelope = utiltest.EnvelopeFromShell(sh, nil, utiltest.HangingApp, "hanging ap", 0, 0, "hAppV1")
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
