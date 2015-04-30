// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/device"
	"v.io/v23/services/stats"
	"v.io/v23/vdl"

	"v.io/x/ref/envvar"
	"v.io/x/ref/services/device/internal/impl/utiltest"
	"v.io/x/ref/services/internal/servicetest"
)

func TestReaperNoticesAppDeath(t *testing.T) {
	cleanup, ctx, sh, envelope, root, helperPath, _ := utiltest.StartupHelper(t)
	defer cleanup()

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh := servicetest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	servicetest.ReadPID(t, dmh)
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", noPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	utiltest.Resolve(t, ctx, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = utiltest.EnvelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.  The config-specified flag value for testFlagName
	// should override the value specified in the envelope above.
	appID := utiltest.InstallApp(t, ctx)

	// Start an instance of the app.
	instance1ID := utiltest.LaunchApp(t, ctx, appID)

	// Wait until the app pings us that it's ready.
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	// Get application pid.
	name := naming.Join("dm", "apps/"+appID+"/"+instance1ID+"/stats/system/pid")
	c := stats.StatsClient(name)
	v, err := c.Value(ctx)
	if err != nil {
		t.Fatalf("Value() failed: %v\n", err)
	}
	var pid int
	if err := vdl.Convert(&pid, v); err != nil {
		t.Fatalf("pid returned from stats interface is not an int: %v", err)
	}

	utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance1ID)
	syscall.Kill(int(pid), 9)

	// Start a second instance of the app which will force polling to happen.
	instance2ID := utiltest.LaunchApp(t, ctx, appID)
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance2ID)

	utiltest.TerminateApp(t, ctx, appID, instance2ID)
	utiltest.VerifyState(t, ctx, device.InstanceStateNotRunning, appID, instance1ID)

	// TODO(rjkroege): Exercise the polling loop code.

	// Cleanly shut down the device manager.
	utiltest.VerifyNoRunningProcesses(t)
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
}

func getPid(t *testing.T, ctx *context.T, appID, instanceID string) int {
	name := naming.Join("dm", "apps/"+appID+"/"+instanceID+"/stats/system/pid")
	c := stats.StatsClient(name)
	v, err := c.Value(ctx)
	if err != nil {
		t.Fatalf("Value() failed: %v\n", err)
	}
	return int(v.Int())
}

func TestReapReconciliation(t *testing.T) {
	cleanup, ctx, sh, envelope, root, helperPath, _ := utiltest.StartupHelper(t)
	defer cleanup()

	// Start a device manager.
	// (Since it will be restarted, use the VeyronCredentials environment
	// to maintain the same set of credentials across runs)
	dmCreds, err := ioutil.TempDir("", "TestDeviceManagerUpdateAndRevert")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dmCreds)
	dmEnv := []string{fmt.Sprintf("%v=%v", envvar.Credentials, dmCreds)}

	dmh := servicetest.RunCommand(t, sh, dmEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	servicetest.ReadPID(t, dmh)
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", noPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()
	utiltest.Resolve(t, ctx, "pingserver", 1)

	// Create an envelope for the app.
	*envelope = utiltest.EnvelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := utiltest.InstallApp(t, ctx)

	// Start three app instances.
	instances := make([]string, 3)
	for i, _ := range instances {
		instances[i] = utiltest.LaunchApp(t, ctx, appID)
		verifyPingArgs(t, pingCh, userName(t), "default", "")
	}

	// Get pid of instance[0]
	pid := getPid(t, ctx, appID, instances[0])

	// Shutdown the first device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
	dmh.Shutdown(os.Stderr, os.Stderr)
	utiltest.ResolveExpectNotFound(t, ctx, "dm") // Ensure a clean slate.

	// Kill instance[0] and wait until it exits before proceeding.
	syscall.Kill(pid, 9)
	timeOut := time.After(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		select {
		case <-timeOut:
			t.Fatalf("Timed out waiting for PID %v to terminate", pid)
		case <-time.After(time.Millisecond):
			// Try again.
		}
	}

	// Run another device manager to replace the dead one.
	dmh = servicetest.RunCommand(t, sh, dmEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	servicetest.ReadPID(t, dmh)
	utiltest.Resolve(t, ctx, "dm", 1) // Verify the device manager has published itself.

	// By now, we've reconciled the state of the tree with which processes
	// are actually alive. instance-0 is not alive.
	expected := []device.InstanceState{device.InstanceStateNotRunning, device.InstanceStateRunning, device.InstanceStateRunning}
	for i, _ := range instances {
		utiltest.VerifyState(t, ctx, expected[i], appID, instances[i])
	}

	// Start instance[0] over-again to show that an app marked not running
	// by reconciliation can be restarted.
	utiltest.RunApp(t, ctx, appID, instances[0])
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	// Kill instance[1]
	pid = getPid(t, ctx, appID, instances[1])
	syscall.Kill(pid, 9)

	// Make a fourth instance. This forces a polling of processes so that
	// the state is updated.
	instances = append(instances, utiltest.LaunchApp(t, ctx, appID))
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	// Stop the fourth instance to make sure that there's no way we could
	// still be running the polling loop before doing the below.
	utiltest.TerminateApp(t, ctx, appID, instances[3])

	// Verify that reaper picked up the previous instances and was watching
	// instance[1]
	expected = []device.InstanceState{device.InstanceStateRunning, device.InstanceStateNotRunning, device.InstanceStateRunning, device.InstanceStateDeleted}
	for i, _ := range instances {
		utiltest.VerifyState(t, ctx, expected[i], appID, instances[i])
	}

	utiltest.TerminateApp(t, ctx, appID, instances[2])

	expected = []device.InstanceState{device.InstanceStateRunning, device.InstanceStateNotRunning, device.InstanceStateDeleted, device.InstanceStateDeleted}
	for i, _ := range instances {
		utiltest.VerifyState(t, ctx, expected[i], appID, instances[i])
	}
	utiltest.TerminateApp(t, ctx, appID, instances[0])

	// TODO(rjkroege): Should be in a defer to ensure that the device
	// manager is cleaned up even if the test fails in an exceptional way.
	utiltest.VerifyNoRunningProcesses(t)
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
}
