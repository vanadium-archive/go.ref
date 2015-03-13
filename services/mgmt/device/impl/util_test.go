package impl_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"v.io/x/lib/vlog"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/services/mgmt/device"
	"v.io/v23/services/mgmt/stats"
	"v.io/v23/verror"

	_ "v.io/x/ref/profiles/roaming"
	"v.io/x/ref/services/mgmt/device/impl"
	mgmttest "v.io/x/ref/services/mgmt/lib/testutil"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
	tsecurity "v.io/x/ref/test/security"
	"v.io/x/ref/test/testutil"
)

const (
	// TODO(caprita): Set the timeout in a more principled manner.
	stopTimeout = 20 // In seconds.
)

func envelopeFromShell(sh *modules.Shell, env []string, cmd, title string, args ...string) application.Envelope {
	args, nenv := sh.CommandEnvelope(cmd, env, args...)
	return application.Envelope{
		Title: title,
		Args:  args[1:],
		// TODO(caprita): revisit how the environment is sanitized for arbirary
		// apps.
		Env:    impl.VeyronEnvironment(nenv),
		Binary: application.SignedFile{File: mockBinaryRepoName},
	}
}

// resolveExpectNotFound verifies that the given name is not in the mounttable.
func resolveExpectNotFound(t *testing.T, ctx *context.T, name string) {
	if me, err := v23.GetNamespace(ctx).Resolve(ctx, name); err == nil {
		t.Fatalf(testutil.FormatLogLine(2, "Resolve(%v) succeeded with results %v when it was expected to fail", name, me.Names))
	} else if expectErr := naming.ErrNoSuchName.ID; !verror.Is(err, expectErr) {
		t.Fatalf(testutil.FormatLogLine(2, "Resolve(%v) failed with error %v, expected error ID %v", name, err, expectErr))
	}
}

// resolve looks up the given name in the mounttable.
func resolve(t *testing.T, ctx *context.T, name string, replicas int) []string {
	me, err := v23.GetNamespace(ctx).Resolve(ctx, name)
	if err != nil {
		t.Fatalf("Resolve(%v) failed: %v", name, err)
	}

	filteredResults := []string{}
	for _, r := range me.Names() {
		if strings.Index(r, "@tcp") != -1 {
			filteredResults = append(filteredResults, r)
		}
	}
	// We are going to get a websocket and a tcp endpoint for each replica.
	if want, got := replicas, len(filteredResults); want != got {
		t.Fatalf("Resolve(%v) expected %d result(s), got %d instead", name, want, got)
	}
	return filteredResults
}

// The following set of functions are convenience wrappers around Update and
// Revert for device manager.

func deviceStub(name string) device.DeviceClientMethods {
	deviceName := naming.Join(name, "device")
	return device.DeviceClient(deviceName)
}

func claimDevice(t *testing.T, ctx *context.T, name, extension, pairingToken string) {
	// Setup blessings to be granted to the claimed device
	g := &granter{p: v23.GetPrincipal(ctx), extension: extension}
	s := options.SkipResolveAuthorization{}
	// Call the Claim RPC: Skip server authorization because the unclaimed
	// device presents nothing that can be used to recognize it.
	if err := device.ClaimableClient(name).Claim(ctx, pairingToken, g, s); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Claim(%q) failed: %v [%v]", name, pairingToken, verror.ErrorID(err), err))
	}
	// Wait for the device to remount itself with the device service after
	// being claimed.
	// (Detected by the next claim failing with an error other than
	// AlreadyClaimed)
	start := time.Now()
	for {
		if err := device.ClaimableClient(name).Claim(ctx, pairingToken, g, s); !verror.Is(err, impl.ErrDeviceAlreadyClaimed.ID) {
			return
		}
		vlog.VI(4).Infof("Claimable server at %q has not stopped yet", name)
		time.Sleep(time.Millisecond)
		if elapsed := time.Since(start); elapsed > time.Minute {
			t.Fatalf("Device hasn't remounted itself in %v since it was claimed", elapsed)
		}
	}
}

func claimDeviceExpectError(t *testing.T, ctx *context.T, name, extension, pairingToken string, errID verror.ID) {
	// Setup blessings to be granted to the claimed device
	g := &granter{p: v23.GetPrincipal(ctx), extension: extension}
	s := options.SkipResolveAuthorization{}
	// Call the Claim RPC
	if err := device.ClaimableClient(name).Claim(ctx, pairingToken, g, s); !verror.Is(err, errID) {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Claim(%q) expected to fail with %v, got %v [%v]", name, pairingToken, errID, verror.ErrorID(err), err))
	}
}

func updateDeviceExpectError(t *testing.T, ctx *context.T, name string, errID verror.ID) {
	if err := deviceStub(name).Update(ctx); !verror.Is(err, errID) {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Update expected to fail with %v, got %v [%v]", name, errID, verror.ErrorID(err), err))
	}
}

func updateDevice(t *testing.T, ctx *context.T, name string) {
	if err := deviceStub(name).Update(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Update() failed: %v [%v]", name, verror.ErrorID(err), err))
	}
}

func revertDeviceExpectError(t *testing.T, ctx *context.T, name string, errID verror.ID) {
	if err := deviceStub(name).Revert(ctx); !verror.Is(err, errID) {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Revert() expected to fail with %v, got %v [%v]", name, errID, verror.ErrorID(err), err))
	}
}

func revertDevice(t *testing.T, ctx *context.T, name string) {
	if err := deviceStub(name).Revert(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Revert() failed: %v [%v]", name, verror.ErrorID(err), err))
	}
}

func stopDevice(t *testing.T, ctx *context.T, name string) {
	if err := deviceStub(name).Stop(ctx, stopTimeout); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Stop(%v) failed: %v [%v]", name, stopTimeout, verror.ErrorID(err), err))
	}
}

func suspendDevice(t *testing.T, ctx *context.T, name string) {
	if err := deviceStub(name).Suspend(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Suspend() failed: %v [%v]", name, verror.ErrorID(err), err))
	}
}

// The following set of functions are convenience wrappers around various app
// management methods.

func ocfg(opt []interface{}) device.Config {
	for _, o := range opt {
		if c, ok := o.(device.Config); ok {
			return c
		}
	}
	return device.Config{}
}

func opkg(opt []interface{}) application.Packages {
	for _, o := range opt {
		if c, ok := o.(application.Packages); ok {
			return c
		}
	}
	return application.Packages{}
}

func appStub(nameComponents ...string) device.ApplicationClientMethods {
	appsName := "dm/apps"
	appName := naming.Join(append([]string{appsName}, nameComponents...)...)
	return device.ApplicationClient(appName)
}

func statsStub(nameComponents ...string) stats.StatsClientMethods {
	baseName := "dm/apps"
	statsName := naming.Join(append([]string{baseName}, nameComponents...)...)
	return stats.StatsClient(statsName)
}

func installApp(t *testing.T, ctx *context.T, opt ...interface{}) string {
	appID, err := appStub().Install(ctx, mockApplicationRepoName, ocfg(opt), opkg(opt))
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Install failed: %v [%v]", verror.ErrorID(err), err))
	}
	return appID
}

func installAppExpectError(t *testing.T, ctx *context.T, expectedError verror.ID, opt ...interface{}) {
	if _, err := appStub().Install(ctx, mockApplicationRepoName, ocfg(opt), opkg(opt)); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Install expected to fail with %v, got %v [%v]", expectedError, verror.ErrorID(err), err))
	}
}

type granter struct {
	ipc.CallOpt
	p         security.Principal
	extension string
}

func (g *granter) Grant(other security.Blessings) (security.Blessings, error) {
	return g.p.Bless(other.PublicKey(), g.p.BlessingStore().Default(), g.extension, security.UnconstrainedUse())
}

func startAppImpl(t *testing.T, ctx *context.T, appID, grant string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	call, err := appStub(appID).Start(ctx)
	if err != nil {
		return "", err
	}
	var instanceIDs []string
	for call.RecvStream().Advance() {
		switch msg := call.RecvStream().Value().(type) {
		case device.StartServerMessageInstanceName:
			instanceIDs = append(instanceIDs, msg.Value)
		case device.StartServerMessageInstancePublicKey:
			p := v23.GetPrincipal(ctx)
			pubKey, err := security.UnmarshalPublicKey(msg.Value)
			if err != nil {
				return "", err
			}
			blessings, err := p.Bless(pubKey, p.BlessingStore().Default(), grant, security.UnconstrainedUse())
			if err != nil {
				return "", errors.New("bless failed")
			}
			call.SendStream().Send(device.StartClientMessageAppBlessings{blessings})
		default:
			return "", fmt.Errorf("startAppImpl: received unexpected message: %#v", msg)
		}
	}
	if err := call.Finish(); err != nil {
		return "", err
	}
	if want, got := 1, len(instanceIDs); want != got {
		t.Fatalf(testutil.FormatLogLine(2, "Start(%v): expected %v instance ids, got %v instead", appID, want, got))
	}
	return instanceIDs[0], nil
}

func startApp(t *testing.T, ctx *context.T, appID string) string {
	instanceID, err := startAppImpl(t, ctx, appID, "forapp")
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Start(%v) failed: %v [%v]", appID, verror.ErrorID(err), err))
	}
	return instanceID
}

func startAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if _, err := startAppImpl(t, ctx, appID, "forapp"); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Start(%v) expected to fail with %v, got %v [%v]", appID, expectedError, verror.ErrorID(err), err))
	}
}

func stopApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := appStub(appID, instanceID).Stop(ctx, stopTimeout); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Stop(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func suspendApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := appStub(appID, instanceID).Suspend(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Suspend(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func resumeApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := appStub(appID, instanceID).Resume(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Resume(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func resumeAppExpectError(t *testing.T, ctx *context.T, appID, instanceID string, expectedError verror.ID) {
	if err := appStub(appID, instanceID).Resume(ctx); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Resume(%v/%v) expected to fail with %v, got %v [%v]", appID, instanceID, expectedError, verror.ErrorID(err), err))
	}
}

func updateInstance(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := appStub(appID, instanceID).Update(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func updateInstanceExpectError(t *testing.T, ctx *context.T, appID, instanceID string, expectedError verror.ID) {
	if err := appStub(appID, instanceID).Update(ctx); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v/%v) expected to fail with %v, got %v [%v]", appID, instanceID, expectedError, verror.ErrorID(err), err))
	}
}

func updateApp(t *testing.T, ctx *context.T, appID string) {
	if err := appStub(appID).Update(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v) failed: %v [%v]", appID, verror.ErrorID(err), err))
	}
}

func updateAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if err := appStub(appID).Update(ctx); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v) expected to fail with %v, got %v [%v]", appID, expectedError, verror.ErrorID(err), err))
	}
}

func revertApp(t *testing.T, ctx *context.T, appID string) {
	if err := appStub(appID).Revert(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Revert(%v) failed: %v [%v]", appID, verror.ErrorID(err), err))
	}
}

func revertAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if err := appStub(appID).Revert(ctx); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Revert(%v) expected to fail with %v, got %v [%v]", appID, expectedError, verror.ErrorID(err), err))
	}
}

func uninstallApp(t *testing.T, ctx *context.T, appID string) {
	if err := appStub(appID).Uninstall(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Uninstall(%v) failed: %v [%v]", appID, verror.ErrorID(err), err))
	}
}

func debug(t *testing.T, ctx *context.T, nameComponents ...string) string {
	dbg, err := appStub(nameComponents...).Debug(ctx)
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Debug(%v) failed: %v [%v]", nameComponents, verror.ErrorID(err), err))
	}
	return dbg
}

// Code to make Association lists sortable.
type byIdentity []device.Association

func (a byIdentity) Len() int           { return len(a) }
func (a byIdentity) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byIdentity) Less(i, j int) bool { return a[i].IdentityName < a[j].IdentityName }

func compareAssociations(t *testing.T, got, expected []device.Association) {
	sort.Sort(byIdentity(got))
	sort.Sort(byIdentity(expected))
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("ListAssociations() got %v, expected %v", got, expected)
	}
}

// generateSuidHelperScript builds a script to execute the test target as
// a suidhelper instance and returns the path to the script.
func generateSuidHelperScript(t *testing.T, root string) string {
	output := "#!/bin/bash\n"
	output += "VEYRON_SUIDHELPER_TEST=1"
	output += " "
	output += "exec " + os.Args[0] + " -minuid=1 -test.run=TestSuidHelper \"$@\""
	output += "\n"

	vlog.VI(1).Infof("script\n%s", output)

	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	path := filepath.Join(root, "helper.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		t.Fatalf("WriteFile(%v) failed: %v", path, err)
	}
	return path
}

// generateAgentScript creates a simple script that acts as the security agent
// for tests.  It blackholes arguments meant for the agent.
func generateAgentScript(t *testing.T, root string) string {
	output := `
#!/bin/bash
ARGS=$*
for ARG in ${ARGS[@]}; do
  if [[ ${ARG} = -- ]]; then
    ARGS=(${ARGS[@]/$ARG})
    break
  elif [[ ${ARG} == --* ]]; then
    ARGS=(${ARGS[@]/$ARG})
  else
    break
  fi
done

exec ${ARGS[@]}
`
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	path := filepath.Join(root, "agenthelper.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		t.Fatalf("WriteFile(%v) failed: %v", path, err)
	}
	return path
}

func ctxWithNewPrincipal(t *testing.T, ctx *context.T, idp *tsecurity.IDProvider, extension string) *context.T {
	ret, err := v23.SetPrincipal(ctx, tsecurity.NewPrincipal())
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "v23.SetPrincipal failed: %v", err))
	}
	if err := idp.Bless(v23.GetPrincipal(ret), extension); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "idp.Bless(?, %q) failed: %v", extension, err))
	}
	return ret
}

// TODO(rjkroege): This helper is generally useful. Use it to reduce
// boiler plate across all device manager tests.
func startupHelper(t *testing.T) (func(), *context.T, *modules.Shell, *application.Envelope, string, string, *tsecurity.IDProvider) {
	ctx, shutdown := test.InitForTest()
	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	// Make a new identity context.
	idp := tsecurity.NewIDProvider("root")
	ctx = ctxWithNewPrincipal(t, ctx, idp, "self")

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)

	// Set up mock application and binary repositories.
	envelope, envCleanup := startMockRepos(t, ctx)

	root, rootCleanup := mgmttest.SetupRootDir(t, "devicemanager")
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	return func() {
		rootCleanup()
		envCleanup()
		deferFn()
		shutdown()
	}, ctx, sh, envelope, root, helperPath, idp
}
