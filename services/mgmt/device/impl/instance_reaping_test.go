package impl_test

import (
	"syscall"
	"testing"
	//	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/services/mgmt/stats"
	verror "v.io/core/veyron2/verror2"

	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/services/mgmt/device/impl"
	mgmttest "v.io/core/veyron/services/mgmt/lib/testutil"
)

func TestReaperNoticesAppDeath(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
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

	// Set up the device manager.  Since we won't do device manager updates,
	// don't worry about its application envelope and current link.
	dmh, dms := mgmttest.RunShellCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dms)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()

	resolve(t, ctx, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.  The config-specified flag value for testFlagName
	// should override the value specified in the envelope above.
	appID := installApp(t, ctx)

	// Start requires the caller to grant a blessing for the app instance.
	if _, err := startAppImpl(t, ctx, appID, ""); err == nil || !verror.Is(err, impl.ErrInvalidBlessing.ID) {
		t.Fatalf("Start(%v) expected to fail with %v, got %v instead", appID, impl.ErrInvalidBlessing.ID, err)
	}

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
	pid, ok := v.(int64)
	if !ok {
		t.Fatalf("pid returned from stats interface is not an int")
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
