// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reaping_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"testing"
	"time"

	"v.io/v23/services/device"
	"v.io/x/ref"
	"v.io/x/ref/services/device/internal/impl"
	"v.io/x/ref/services/device/internal/impl/utiltest"
	"v.io/x/ref/services/internal/servicetest"
)

func TestReapReconciliationViaAppCycle(t *testing.T) {
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
	dmEnv := []string{fmt.Sprintf("%v=%v", ref.EnvCredentials, dmCreds), fmt.Sprintf("%v=%v", impl.AppcycleReconciliation, "1")}

	dmh := servicetest.RunCommand(t, sh, dmEnv, utiltest.DeviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	servicetest.ReadPID(t, dmh)
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", utiltest.NoPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := utiltest.SetupPingServer(t, ctx)
	defer cleanup()
	utiltest.Resolve(t, ctx, "pingserver", 1)

	// Create an envelope for the app.
	*envelope = utiltest.EnvelopeFromShell(sh, nil, utiltest.AppCmd, "google naps", 0, 0, "appV1")

	// Install the app.
	appID := utiltest.InstallApp(t, ctx)

	// Start three app instances.
	instances := make([]string, 3)
	for i, _ := range instances {
		instances[i] = utiltest.LaunchApp(t, ctx, appID)
		pingCh.VerifyPingArgs(t, utiltest.UserName(t), "default", "")
	}

	// Get pid of instance[0]
	pid := utiltest.GetPid(t, ctx, appID, instances[0])

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
	dmh = servicetest.RunCommand(t, sh, dmEnv, utiltest.DeviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
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
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "default", "")

	// Kill instance[1]
	pid = utiltest.GetPid(t, ctx, appID, instances[1])
	syscall.Kill(pid, 9)

	// Make a fourth instance. This forces a polling of processes so that
	// the state is updated.
	instances = append(instances, utiltest.LaunchApp(t, ctx, appID))
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "default", "")

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
