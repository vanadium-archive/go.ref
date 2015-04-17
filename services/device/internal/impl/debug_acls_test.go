// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl_test

import (
	"io/ioutil"
	"syscall"
	"testing"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/permissions"
	"v.io/v23/verror"

	"v.io/x/ref/services/internal/servicetest"
	"v.io/x/ref/test/testutil"
)

func updateAccessList(t *testing.T, ctx *context.T, blessing, right string, name ...string) {
	accessStub := permissions.ObjectClient(naming.Join(name...))
	acl, version, err := accessStub.GetPermissions(ctx)
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "GetPermissions(%v) failed %v", name, err))
	}
	acl.Add(security.BlessingPattern(blessing), right)
	if err = accessStub.SetPermissions(ctx, acl, version); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "SetPermissions(%v, %v, %v) failed: %v", name, blessing, right, err))
	}
}

func testAccessFail(t *testing.T, expected verror.ID, ctx *context.T, who string, name ...string) {
	if _, err := statsStub(name...).Value(ctx); verror.ErrorID(err) != expected {
		t.Fatalf(testutil.FormatLogLine(2, "%s got error %v but expected %v", who, err, expected))
	}
}

func TestDebugPermissionsPropagation(t *testing.T) {
	cleanup, ctx, sh, envelope, root, helperPath, idp := startupHelper(t)
	defer cleanup()

	// Set up the device manager.
	dmh := servicetest.RunCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	servicetest.ReadPID(t, dmh)
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
	updateAccessList(t, selfCtx, "root/bob/$", string(access.Read), "dm/apps", appID)

	// Bob starts an instance of the app.
	bobApp := launchApp(t, bobCtx, appID)
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	// Bob permits Alice to read from his app.
	updateAccessList(t, bobCtx, "root/alice/$", string(access.Read), "dm/apps", appID, bobApp)

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
	appGlobtests := []globTestVector{
		{naming.Join("appV1", "__debug"), "*",
			[]string{"logs", "pprof", "stats", "vtrace"},
		},
		{naming.Join("appV1", "__debug", "stats", "system"),
			"start-time*",
			[]string{"start-time-rfc1123", "start-time-unix"},
		},
		{naming.Join("appV1", "__debug", "logs"),
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

	// Bob started the app so selfCtx can't connect to the app.
	verifyFailGlob(t, selfCtx, appGlobtests)
	testAccessFail(t, verror.ErrNoAccess.ID, selfCtx, "self", "appV1", "__debug", "stats/system/pid")

	// hackerjoe (for example) can't either.
	verifyFailGlob(t, hjCtx, appGlobtests)
	testAccessFail(t, verror.ErrNoAccess.ID, hjCtx, "hackerjoe", "appV1", "__debug", "stats/system/pid")

	// Bob has an issue with his app and tries to use the debug output to figure it out.
	verifyGlob(t, bobCtx, "app", globtests, res)
	verifyStatsValues(t, bobCtx, "dm", "apps", appID, bobApp, "stats/system/start-time*")
	verifyLog(t, bobCtx, "dm", "apps", appID, bobApp, "logs", "*")
	verifyPProfCmdLine(t, bobCtx, "app", "dm", "apps", appID, bobApp, "pprof")

	// Bob can also connect directly to his app.
	verifyGlob(t, bobCtx, "app", appGlobtests, res)
	verifyStatsValues(t, bobCtx, "appV1", "__debug", "stats/system/start-time*")

	// But Bob can't figure it out and hopes that hackerjoe can debug it.
	updateAccessList(t, bobCtx, "root/hackerjoe/$", string(access.Debug), "dm/apps", appID, bobApp)

	// Fortunately the device manager permits hackerjoe to access the stats.
	// But hackerjoe can't solve Bob's problem.
	// Because hackerjoe has Debug, hackerjoe can glob the __debug resources
	// of Bob's app but can't glob Bob's app.
	verifyGlob(t, hjCtx, "app", globtestminus, res)
	verifyFailGlob(t, hjCtx, globtests[0:1])
	verifyStatsValues(t, hjCtx, "dm", "apps", appID, bobApp, "stats", "system/start-time*")
	verifyLog(t, hjCtx, "dm", "apps", appID, bobApp, "logs", "*")
	verifyPProfCmdLine(t, hjCtx, "app", "dm", "apps", appID, bobApp, "pprof")

	// Permissions are propagated to the app so hackerjoe can connect
	// directly to the app too.
	verifyGlob(t, hjCtx, "app", globtestminus, res)
	verifyStatsValues(t, hjCtx, "appV1", "__debug", "stats/system/start-time*")

	// Alice might be able to help but Bob didn't give Alice access to the debug ACLs.
	testAccessFail(t, verror.ErrNoAccess.ID, aliceCtx, "Alice", "dm", "apps", appID, bobApp, "stats/system/pid")

	// Bob forgets that Alice can't read the stats when he can.
	verifyGlob(t, bobCtx, "app", globtests, res)
	verifyStatsValues(t, bobCtx, "dm", "apps", appID, bobApp, "stats/system/start-time*")

	// So Bob changes the permissions so that Alice can help debug too.
	updateAccessList(t, bobCtx, "root/alice/$", string(access.Debug), "dm/apps", appID, bobApp)

	// Alice can access __debug content.
	verifyGlob(t, aliceCtx, "app", globtestminus, res)
	verifyFailGlob(t, aliceCtx, globtests[0:1])
	verifyStatsValues(t, aliceCtx, "dm", "apps", appID, bobApp, "stats", "system/start-time*")
	verifyLog(t, aliceCtx, "dm", "apps", appID, bobApp, "logs", "*")
	verifyPProfCmdLine(t, aliceCtx, "app", "dm", "apps", appID, bobApp, "pprof")

	// Alice can also now connect directly to the app.
	verifyGlob(t, aliceCtx, "app", globtestminus, res)
	verifyStatsValues(t, aliceCtx, "appV1", "__debug", "stats/system/start-time*")

	// Bob is glum because no one can help him fix his app so he terminates
	// it.
	terminateApp(t, bobCtx, appID, bobApp)

	// Cleanly shut down the device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
}

func TestClaimSetsDebugPermissions(t *testing.T) {
	cleanup, ctx, sh, _, root, helperPath, idp := startupHelper(t)
	defer cleanup()

	extraLogDir, err := ioutil.TempDir(root, "testlogs")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}

	// Set up the device manager.
	dmh := servicetest.RunCommand(t, sh, nil, deviceManagerCmd, "--log_dir="+extraLogDir, "dm", root, helperPath, "unused", "unused_curr_link")
	servicetest.ReadPID(t, dmh)

	// Make some users.
	selfCtx := ctx
	bobCtx := ctxWithNewPrincipal(t, selfCtx, idp, "bob")
	aliceCtx := ctxWithNewPrincipal(t, selfCtx, idp, "alice")
	hjCtx := ctxWithNewPrincipal(t, selfCtx, idp, "hackerjoe")

	// Bob claims the device manager.
	claimDevice(t, bobCtx, "dm", "mydevice", noPairingToken)

	// Create some globbing test vectors.
	dmGlobtests := []globTestVector{
		{naming.Join("dm", "__debug"), "*",
			[]string{"logs", "pprof", "stats", "vtrace"},
		},
		{naming.Join("dm", "__debug", "stats", "system"),
			"start-time*",
			[]string{"start-time-rfc1123", "start-time-unix"},
		},
		{naming.Join("dm", "__debug", "logs"),
			"*",
			[]string{
				// STDERR and STDOUT are not handled through the log package so
				// are not included here.
				"impl.test.INFO",
				"impl.test.<*>.INFO.<timestamp>",
			},
		},
	}
	res := newGlobTestRegexHelper(`impl\.test`)

	// Bob claimed the DM so can access it.
	verifyGlob(t, bobCtx, "impl.test", dmGlobtests, res)
	verifyStatsValues(t, bobCtx, "dm", "__debug", "stats/system/start-time*")

	// Without permissions, hackerjoe can't access the device manager.
	verifyFailGlob(t, hjCtx, dmGlobtests)
	testAccessFail(t, verror.ErrNoAccess.ID, hjCtx, "hackerjoe", "dm", "__debug", "stats/system/pid")

	// Bob gives system administrator Alice admin access to the dm and hence Alice
	// can access the __debug space.
	updateAccessList(t, bobCtx, "root/alice/$", string(access.Admin), "dm", "device")

	// Alice is an adminstrator and so can can access device manager __debug
	// values.
	verifyGlob(t, aliceCtx, "impl.test", dmGlobtests, res)
	verifyStatsValues(t, aliceCtx, "dm", "__debug", "stats/system/start-time*")

	// Bob gives debug access to the device manager to hackerjoe
	updateAccessList(t, bobCtx, "root/hackerjoe/$", string(access.Debug), "dm", "device")

	// hackerjoe can now access the device manager
	verifyGlob(t, hjCtx, "impl.test", dmGlobtests, res)
	verifyStatsValues(t, hjCtx, "dm", "__debug", "stats/system/start-time*")

	// Cleanly shut down the device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dmh.Expect("dm terminated")
	dmh.ExpectEOF()
}
