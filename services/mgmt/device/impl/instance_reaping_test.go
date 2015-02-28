package impl_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/services/mgmt/stats"
	"v.io/v23/vdl"

	"v.io/core/veyron/lib/flags/consts"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	mgmttest "v.io/core/veyron/services/mgmt/lib/testutil"
)

// TODO(rjkroege): This helper is generally useful. Move to util_test.go
// and use it to reduce boiler plate across all tests here.
func startupHelper(t *testing.T) (func(), *context.T, *modules.Shell, *application.Envelope, string, string) {
	ctx, shutdown := testutil.InitForTest()
	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)

	// Set up mock application and binary repositories.
	envelope, envCleanup := startMockRepos(t, ctx)

	root, rootCleanup := mgmttest.SetupRootDir(t, "devicemanager")

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	return func() {
		rootCleanup()
		envCleanup()
		deferFn()
		shutdown()
	}, ctx, sh, envelope, root, helperPath
}

func TestReaperNoticesAppDeath(t *testing.T) {
	cleanup, ctx, sh, envelope, root, helperPath := startupHelper(t)
	defer cleanup()

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh, dms := mgmttest.RunShellCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dms)
	claimDevice(t, ctx, "dm", "mydevice", noPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	resolve(t, ctx, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.  The config-specified flag value for testFlagName
	// should override the value specified in the envelope above.
	appID := installApp(t, ctx)

	// Start an instance of the app.
	instance1ID := startApp(t, ctx, appID)

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

	verifyAppState(t, root, appID, instance1ID, "started")
	syscall.Kill(int(pid), 9)

	// Start a second instance of the app which will force polling to happen.
	instance2ID := startApp(t, ctx, appID)
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	verifyAppState(t, root, appID, instance2ID, "started")

	stopApp(t, ctx, appID, instance2ID)
	verifyAppState(t, root, appID, instance1ID, "suspended")

	// TODO(rjkroege): Exercise the polling loop code.

	// Cleanly shut down the device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dms.Expect("dm terminated")
	dms.ExpectEOF()
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
	cleanup, ctx, sh, envelope, root, helperPath := startupHelper(t)
	defer cleanup()

	// Start a device manager.
	// (Since it will be restarted, use the VeyronCredentials environment
	// to maintain the same set of credentials across runs)
	dmCreds, err := ioutil.TempDir("", "TestDeviceManagerUpdateAndRevert")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dmCreds)
	dmEnv := []string{fmt.Sprintf("%v=%v", consts.VeyronCredentials, dmCreds)}

	dmh, dms := mgmttest.RunShellCommand(t, sh, dmEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dms)
	claimDevice(t, ctx, "dm", "mydevice", noPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()
	resolve(t, ctx, "pingserver", 1)

	// Create an envelope for the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := installApp(t, ctx)

	// Start three app instances.
	instances := make([]string, 3)
	for i, _ := range instances {
		instances[i] = startApp(t, ctx, appID)
		verifyPingArgs(t, pingCh, userName(t), "default", "")
	}

	// Get pid of instance[0]
	pid := getPid(t, ctx, appID, instances[0])

	// Shutdown the first device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dms.Expect("dm terminated")
	dms.ExpectEOF()
	dmh.Shutdown(os.Stderr, os.Stderr)
	resolveExpectNotFound(t, ctx, "dm") // Ensure a clean slate.

	// Kill instance[0]
	syscall.Kill(pid, 9)

	// The device manager is dead so there will be no updates of the status.
	for _, instance := range instances {
		verifyAppState(t, root, appID, instance, "started")
	}

	// Run another device manager to replace the dead one.
	dmh, dms = mgmttest.RunShellCommand(t, sh, dmEnv, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dms)
	resolve(t, ctx, "dm", 1) // Verify the device manager has published itself.

	// By now, we've reconciled the state of the tree with which processes are actually
	// alive. instance-0 is not alive.
	expected := []string{"suspended", "started", "started"}
	for i, _ := range instances {
		verifyAppState(t, root, appID, instances[i], expected[i])
	}

	// Start instance[0] over-again to show that an app suspended by reconciliation can
	// be restarted.
	resumeApp(t, ctx, appID, instances[0])
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	// Kill instance[1]
	pid = getPid(t, ctx, appID, instances[1])
	syscall.Kill(pid, 9)

	// Make a fourth instance. This forces a polling of processes so that the state is updated.
	instances = append(instances, startApp(t, ctx, appID))
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	// Stop the fourth instance to make sure that there's no way we could still be
	// running the polling loop before doing the below.
	stopApp(t, ctx, appID, instances[3])

	// Verify that reaper picked up the previous instances and was watching instance[1]
	expected = []string{"started", "suspended", "started", "stopped"}
	for i, _ := range instances {
		verifyAppState(t, root, appID, instances[i], expected[i])
	}

	stopApp(t, ctx, appID, instances[2])
	expected = []string{"started", "suspended", "stopped", "stopped"}
	for i, _ := range instances {
		verifyAppState(t, root, appID, instances[i], expected[i])
	}
	stopApp(t, ctx, appID, instances[0])

	// TODO(rjkroege): Should be in a defer to ensure that the device manager
	// is cleaned up even if the test fails in an exceptional way.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dms.Expect("dm terminated")
	dms.ExpectEOF()
}
