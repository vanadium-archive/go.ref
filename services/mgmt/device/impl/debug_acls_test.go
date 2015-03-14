package impl_test

import (
	"syscall"
	"testing"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"

	mgmttest "v.io/x/ref/services/mgmt/lib/testutil"
	"v.io/x/ref/test/testutil"
)

func updateAccessList(t *testing.T, ctx *context.T, blessing, right string, name ...string) {
	acl, etag, err := appStub(name...).GetPermissions(ctx)
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "GetACL(%v) failed %v", name, err))
	}
	acl.Add(security.BlessingPattern(blessing), right)
	if err = appStub(name...).SetPermissions(ctx, acl, etag); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "SetPermissions(%v, %v, %v) failed: %v", name, blessing, right, err))
	}
}

func mn(base []string, suffix ...string) []string {
	fn := make([]string, len(base))
	// Must copy because append will modify the original.
	copy(fn, base)
	fn = append(fn, suffix...)
	return fn
}

func testAccessFail(t *testing.T, expected verror.ID, ctx *context.T, who string, name ...string) {
	if _, err := statsStub(mn(name, "stats/system/pid")...).Value(ctx); !verror.Is(err, expected) {
		t.Fatalf(testutil.FormatLogLine(2, "%s got error %v but expected %v", who, err, expected))
	}
}

func TestDebugPermissionsPropagation(t *testing.T) {
	cleanup, ctx, sh, envelope, root, helperPath, idp := startupHelper(t)
	defer cleanup()

	// Set up the device manager.
	dmh := mgmttest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dmh)
	claimDevice(t, ctx, "dm", "mydevice", noPairingToken)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t, ctx)
	defer cleanup()
	resolve(t, ctx, "pingserver", 1)

	// Make some users.
	selfCtx := ctx
	bobCtx := ctxWithNewPrincipal(t, selfCtx, idp, "bob")
	hjCtx := ctxWithNewPrincipal(t, selfCtx, idp, "hackerjoe")
	aliceCtx := ctxWithNewPrincipal(t, selfCtx, idp, "alice")

	// TODO(rjkroege): Set AccessLists here that conflict with the one provided by the device
	// manager and show that the one set here is overridden.
	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := installApp(t, ctx)

	// Give bob rights to start an app.
	updateAccessList(t, selfCtx, "root/bob/$", string(access.Read), appID)

	// Bob starts an instance of the app.
	bobApp := startApp(t, bobCtx, appID)
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	// Bob permits Alice to read from his app.
	updateAccessList(t, bobCtx, "root/alice/$", string(access.Read), appID, bobApp)

	// Create some globbing test vectors.
	globtests := []globTestVector{
		{naming.Join("dm", "apps", appID, bobApp), "*",
			[]string{"logs", "pprof", "stats"},
		},
		{naming.Join("dm", "apps", appID, bobApp, "stats", "system"),
			"start-time*",
			[]string{"start-time-rfc1123", "start-time-unix"},
		},
		{naming.Join("dm", "apps", appID, bobApp, "logs"),
			"*",
			[]string{
				"STDERR-<timestamp>",
				"STDOUT-<timestamp>",
				"app.INFO",
				"app.<*>.INFO.<timestamp>",
			},
		},
	}
	globtestminus := globtests[1:]
	res := newGlobTestRegexHelper("app")

	// Confirm that self can access __debug names.
	verifyGlob(t, selfCtx, "app", globtests, res)
	verifyStatsValues(t, selfCtx, "dm", "apps", appID, bobApp, "stats/system/start-time*")
	verifyLog(t, selfCtx, "dm", "apps", appID, bobApp, "logs", "*")
	verifyPProfCmdLine(t, selfCtx, "app", "dm", "apps", appID, bobApp, "pprof")

	// Bob has an issue with his app and tries to use the debug output to figure it out.
	verifyGlob(t, bobCtx, "app", globtests, res)
	verifyStatsValues(t, bobCtx, "dm", "apps", appID, bobApp, "stats/system/start-time*")
	verifyLog(t, bobCtx, "dm", "apps", appID, bobApp, "logs", "*")
	verifyPProfCmdLine(t, bobCtx, "app", "dm", "apps", appID, bobApp, "pprof")

	// But Bob can't figure it out and hopes that hackerjoe can debug it.
	updateAccessList(t, bobCtx, "root/hackerjoe/$", string(access.Debug), appID, bobApp)

	// Fortunately the device manager permits hackerjoe to access the stats.
	// But hackerjoe can't solve Bob's problem.
	// Because hackerjob has Debug, hackerjoe can glob the __debug resources
	// of Bob's app but can't glob Bob's app.
	verifyGlob(t, hjCtx, "app", globtestminus, res)
	verifyFailGlob(t, hjCtx, "app", globtests[0:1], res)
	verifyStatsValues(t, hjCtx, "dm", "apps", appID, bobApp, "stats", "system/start-time*")
	verifyLog(t, hjCtx, "dm", "apps", appID, bobApp, "logs", "*")
	verifyPProfCmdLine(t, hjCtx, "app", "dm", "apps", appID, bobApp, "pprof")

	// Alice might be able to help but Bob didn't give Alice access to the debug ACLs.
	testAccessFail(t, verror.ErrNoAccess.ID, aliceCtx, "Alice", appID, bobApp)

	// Bob forgets that Alice can't read the stats when he can.
	verifyGlob(t, bobCtx, "app", globtests, res)
	verifyStatsValues(t, bobCtx, "dm", "apps", appID, bobApp, "stats/system/start-time*")

	// So Bob changes the permissions so that Alice can help debug too.
	updateAccessList(t, bobCtx, "root/alice/$", string(access.Debug), appID, bobApp)

	// Alice can access __debug content.
	verifyGlob(t, aliceCtx, "app", globtestminus, res)
	verifyFailGlob(t, aliceCtx, "app", globtests[0:1], res)
	verifyStatsValues(t, aliceCtx, "dm", "apps", appID, bobApp, "stats", "system/start-time*")
	verifyLog(t, aliceCtx, "dm", "apps", appID, bobApp, "logs", "*")
	verifyPProfCmdLine(t, aliceCtx, "app", "dm", "apps", appID, bobApp, "pprof")

	// Bob is glum because no one can help him fix his app so he stops it.
	stopApp(t, bobCtx, appID, bobApp)

	// Cleanly shut down the device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
}
