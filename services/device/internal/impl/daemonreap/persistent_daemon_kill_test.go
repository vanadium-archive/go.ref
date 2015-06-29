// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package daemonreap_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"testing"
	"time"

	"v.io/v23/services/device"
	"v.io/x/ref"
	"v.io/x/ref/services/device/internal/impl/utiltest"
	"v.io/x/ref/services/internal/servicetest"
)

func TestReapRestartsDaemonMode(t *testing.T) {
	// TODO(rjkroege): Enable this test once v.io/i/573 is fixed.
	t.Skip("Test is flaky. Disabling until v.io/i/573 is fixed.")

	cleanup, ctx, sh, envelope, root, helperPath, _ := utiltest.StartupHelper(t)
	defer cleanup()

	// Start a device manager.
	// (Since it will be restarted, use the VeyronCredentials environment
	// to maintain the same set of credentials across runs)
	dmCreds, err := ioutil.TempDir("", "TestReapReconciliationViaKill")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dmCreds)
	dmEnv := []string{fmt.Sprintf("%v=%v", ref.EnvCredentials, dmCreds)}

	dmh := servicetest.RunCommand(t, sh, dmEnv, utiltest.DeviceManager, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	servicetest.ReadPID(t, dmh)
	utiltest.ClaimDevice(t, ctx, "claimable", "dm", "mydevice", utiltest.NoPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := utiltest.SetupPingServer(t, ctx)
	defer cleanup()
	utiltest.Resolve(t, ctx, "pingserver", 1, true)

	// Create an envelope for a daemon app.
	*envelope = utiltest.EnvelopeFromShell(sh, nil, utiltest.App, "google naps", 10, time.Hour, "appV1")

	// Install the app.
	appID := utiltest.InstallApp(t, ctx)

	instance1 := utiltest.LaunchApp(t, ctx, appID)
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "default", "")

	// Get pid of first instance.
	pid := utiltest.GetPid(t, ctx, appID, instance1)

	// Shutdown the first device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
	dmh.Shutdown(os.Stderr, os.Stderr)
	utiltest.ResolveExpectNotFound(t, ctx, "dm", false) // Ensure a clean slate.

	// Kill instance[0] and wait until it exits before proceeding.
	syscall.Kill(pid, 9)
	utiltest.PollingWait(t, int(pid))

	// Run another device manager to replace the dead one.
	dmh = servicetest.RunCommand(t, sh, dmEnv, utiltest.DeviceManager, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")

	defer func() {
		utiltest.TerminateApp(t, ctx, appID, instance1)
		utiltest.VerifyNoRunningProcesses(t)
		syscall.Kill(dmh.Pid(), syscall.SIGINT)
		dmh.Expect("dm terminated")
		dmh.ExpectEOF()
	}()

	servicetest.ReadPID(t, dmh)
	utiltest.Resolve(t, ctx, "dm", 1, true) // Verify the device manager has published itself.

	// The app will ping us. Wait for it.
	pingCh.VerifyPingArgs(t, utiltest.UserName(t), "default", "")

	// By now, we've reconciled the state of the tree with which processes
	// are actually alive. instance1 was not alive but since it is configured as a
	// daemon, it will have been restarted.
	utiltest.VerifyState(t, ctx, device.InstanceStateRunning, appID, instance1)
}
