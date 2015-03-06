package impl_test

import (
	"syscall"
	"testing"

	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/vdl"

	mgmttest "v.io/x/ref/services/mgmt/lib/testutil"
)

func updateACL(t *testing.T, ctx *context.T, blessing, right string, name ...string) {
	acl, etag, err := appStub(name...).GetACL(ctx)
	if err != nil {
		t.Fatalf("GetACL(%v) failed %v", name, err)
	}
	acl.Add(security.BlessingPattern(blessing), right)
	if err = appStub(name...).SetACL(ctx, acl, etag); err != nil {
		t.Fatalf("SetACL(%v, %v, %v) failed: %v", name, blessing, right, err)
	}
}

func TestDebugACLPropagation(t *testing.T) {
	cleanup, ctx, sh, envelope, root, helperPath, idp := startupHelper(t)
	defer cleanup()

	// Set up the device manager.
	dmh, dms := mgmttest.RunShellCommand(t, sh, nil, deviceManagerCmd, "dm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	mgmttest.ReadPID(t, dms)
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

	// TODO(rjkroege): Set ACLs here that conflict with the one provided by the device
	// manager and show that the one set here is overridden.
	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := installApp(t, ctx)

	// Give bob rights to start an app.
	updateACL(t, selfCtx, "root/bob/$", string(access.Read), appID)

	// Bob starts an instance of the app.
	bobApp := startApp(t, bobCtx, appID)
	verifyPingArgs(t, pingCh, userName(t), "default", "")

	// Bob permits Alice to read from his app.
	updateACL(t, bobCtx, "root/alice/$", string(access.Read), appID, bobApp)

	// Confirm that self can access stats name (i.e. apps proxied from
	// the __debug space of the app.
	// TODO(rjkroege): validate each of the services under __debug.
	v, err := statsStub(appID, bobApp, "stats/system/pid").Value(ctx)
	if err != nil {
		t.Fatalf("Value() failed: %v\n", err)
	}
	var pid int
	if err := vdl.Convert(&pid, v); err != nil {
		t.Fatalf("pid returned from stats interface is not an int: %v", err)
	}

	// Bob has an issue with his app and tries to use the debug output to figure it out.
	// TODO(rjkroege): Invert this test when ACLs are correctly propagated.
	if _, err = statsStub(appID, bobApp, "stats/system/pid").Value(bobCtx); err == nil {
		t.Fatalf("This ought not to work yet! Something is wrong.")
	}

	// But Bob can't figure it out and hopes that hackerjoe can debug it.
	updateACL(t, bobCtx, "root/hackerjoe/$", string(access.Debug), appID, bobApp)

	// But the device manager doesn't support this so hackerjoe can't help.
	// TODO(rjkroege): Invert this test when ACLs are propagated..
	if _, err = statsStub(appID, bobApp, "stats/system/pid").Value(hjCtx); err == nil {
		t.Fatalf("This ought not to work yet! Something is wrong.")
	}

	// Alice might be able to help but Bob didn't give Alice access to the debug ACLs.
	if _, err = statsStub(appID, bobApp, "stats/system/pid").Value(hjCtx); err == nil {
		t.Fatalf("Alice could wrongly access the stats without perms.")
	}

	// Bob changes the permissions so that Alice can help debug too.
	updateACL(t, bobCtx, "root/alice/$", string(access.Debug), appID, bobApp)

	// But the device manager doesn't support this so Alice can't help either.
	// TODO(rjkroege): Invert this test when ACLs are propagated..
	if _, err = statsStub(appID, bobApp, "stats/system/pid").Value(aliceCtx); err == nil {
		t.Fatalf("This ought not to work yet! Something is wrong.")
	}

	// Bob is glum so stops his app.
	stopApp(t, bobCtx, appID, bobApp)

	// Cleanly shut down the device manager.
	syscall.Kill(dmh.Pid(), syscall.SIGINT)
	dms.Expect("dm terminated")
	dms.ExpectEOF()
}
