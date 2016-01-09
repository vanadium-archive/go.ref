// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package daemonreap_test

import (
	"os"
	"syscall"
	"testing"
	"time"

	"v.io/v23/services/device"
	"v.io/x/ref/services/device/deviced/internal/impl/utiltest"
)

func TestDaemonRestart(t *testing.T) {
	if raceEnabled {
		t.Skip("Test is flaky when run with -race.  Disabling until v.io/i/573 is fixed.")
	}
	cleanup, ctx, sh, envelope, root, helperPath, _ := utiltest.StartupHelper(t)
	defer cleanup()

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dm := utiltest.DeviceManagerCmd(sh, utiltest.DeviceManager, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	dm.Start()
	dm.S.Expect("READY")
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", utiltest.NoPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := utiltest.SetupPingServer(t, ctx)
	defer cleanup()

	utiltest.Resolve(t, ctx, "pingserver", 1, true)

	// Create an envelope for a first version of the app that will be restarted once.
	*envelope = utiltest.EnvelopeFromShell(sh, nil, nil, utiltest.App, "google naps", 1, 10*time.Minute, "appV1")
	appID := utiltest.InstallApp(t, ctx)

	// Start an instance of the app.
	instance1ID := utiltest.LaunchApp(t, ctx, appID)

	// Wait until the app pings us that it's ready.
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "default", "")

	// Get application pid.
	pid := utiltest.GetPid(t, ctx, appID, instance1ID)

	utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance1ID)
	syscall.Kill(int(pid), 9)
	utiltest.PollingWait(t, int(pid))

	// Start a second instance of the app which will force polling to happen.
	// During this polling, the reaper will restart app instance1
	instance2ID := utiltest.LaunchApp(t, ctx, appID)
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "default", "")
	utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance2ID)

	// Stop the second instance of the app which will also force polling. By this point,
	// instance1 should be live.
	utiltest.KillApp(t, ctx, appID, instance2ID)

	// instance2ID is not running.
	utiltest.VerifyState(t, ctx, device.InstanceStateNotRunning, appID, instance2ID)

	// Be sure to get the ping from the restarted application so that the app is running
	// again before we ask for its status.
	pingCh.WaitForPingArgs(t)

	// instance1ID was restarted automatically.
	utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance1ID)

	// Get application pid.
	pid = utiltest.GetPid(t, ctx, appID, instance1ID)
	// Kill the application again.
	syscall.Kill(int(pid), 9)
	utiltest.PollingWait(t, int(pid))

	// Start and stop instance 2 again to force two polling cycles.
	utiltest.RunApp(t, ctx, appID, instance2ID)
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "default", "")
	utiltest.KillApp(t, ctx, appID, instance2ID)

	// instance1ID is not running because it exceeded its restart limit.
	utiltest.VerifyState(t, ctx, device.InstanceStateNotRunning, appID, instance1ID)

	// instance2ID is not running.
	utiltest.VerifyState(t, ctx, device.InstanceStateNotRunning, appID, instance2ID)

	// Cleanly shut down the device manager.
	dm.Shutdown(os.Interrupt)
	dm.S.Expect("dm terminated")
	utiltest.VerifyNoRunningProcesses(t)
}
