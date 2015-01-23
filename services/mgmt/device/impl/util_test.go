package impl_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/device"
	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	_ "v.io/core/veyron/profiles/static"
	"v.io/core/veyron/services/mgmt/device/impl"
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
		Binary: mockBinaryRepoName,
	}
}

// resolveExpectNotFound verifies that the given name is not in the mounttable.
func resolveExpectNotFound(t *testing.T, ctx *context.T, name string) {
	if me, err := veyron2.GetNamespace(ctx).Resolve(ctx, name); err == nil {
		t.Fatalf(testutil.FormatLogLine(2, "Resolve(%v) succeeded with results %v when it was expected to fail", name, me.Names))
	} else if expectErr := naming.ErrNoSuchName.ID; !verror2.Is(err, expectErr) {
		t.Fatalf(testutil.FormatLogLine(2, "Resolve(%v) failed with error %v, expected error ID %v", name, err, expectErr))
	}
}

// resolve looks up the given name in the mounttable.
func resolve(t *testing.T, ctx *context.T, name string, replicas int) []string {
	me, err := veyron2.GetNamespace(ctx).Resolve(ctx, name)
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

func updateDeviceExpectError(t *testing.T, ctx *context.T, name string, errID verror.ID) {
	if err := deviceStub(name).Update(ctx); !verror2.Is(err, errID) {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v) expected to fail with %v, got %v instead", name, errID, err))
	}
}

func updateDevice(t *testing.T, ctx *context.T, name string) {
	if err := deviceStub(name).Update(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v) failed: %v", name, err))
	}
}

func revertDeviceExpectError(t *testing.T, ctx *context.T, name string, errID verror.ID) {
	if err := deviceStub(name).Revert(ctx); !verror2.Is(err, errID) {
		t.Fatalf(testutil.FormatLogLine(2, "Revert(%v) expected to fail with %v, got %v instead", name, errID, err))
	}
}

func revertDevice(t *testing.T, ctx *context.T, name string) {
	if err := deviceStub(name).Revert(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Revert(%v) failed: %v", name, err))
	}
}

func stopDevice(t *testing.T, ctx *context.T, name string) {
	if err := deviceStub(name).Stop(ctx, stopTimeout); err != nil {
		t.Fatalf(testutil.FormatLogLine(1+1, "%s: Stop(%v) failed: %v", name, err))
	}
}

func suspendDevice(t *testing.T, ctx *context.T, name string) {
	if err := deviceStub(name).Suspend(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(1+1, "%s: Suspend(%v) failed: %v", name, err))
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

func appStub(nameComponents ...string) device.ApplicationClientMethods {
	appsName := "dm//apps"
	appName := naming.Join(append([]string{appsName}, nameComponents...)...)
	return device.ApplicationClient(appName)
}

func installApp(t *testing.T, ctx *context.T, opt ...interface{}) string {
	appID, err := appStub().Install(ctx, mockApplicationRepoName, ocfg(opt))
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Install failed: %v", err))
	}
	return appID
}

func installAppExpectError(t *testing.T, ctx *context.T, expectedError verror.ID, opt ...interface{}) {
	if _, err := appStub().Install(ctx, mockApplicationRepoName, ocfg(opt)); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Install expected to fail with %v, got %v instead", expectedError, err))
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
	var opts []ipc.CallOpt
	if grant != "" {
		opts = append(opts, &granter{p: veyron2.GetPrincipal(ctx), extension: grant})
	}
	if instanceIDs, err := appStub(appID).Start(ctx, opts...); err != nil {
		return "", err
	} else {
		if want, got := 1, len(instanceIDs); want != got {
			t.Fatalf(testutil.FormatLogLine(2, "Start(%v): expected %v instance ids, got %v instead", appID, want, got))
		}
		return instanceIDs[0], nil
	}
}

func startApp(t *testing.T, ctx *context.T, appID string) string {
	instanceID, err := startAppImpl(t, ctx, appID, "forapp")
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Start(%v) failed: %v", appID, err))
	}
	return instanceID
}

func startAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if _, err := startAppImpl(t, ctx, appID, "forapp"); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Start(%v) expected to fail with %v, got %v instead", appID, expectedError, err))
	}
}

func stopApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := appStub(appID, instanceID).Stop(ctx, stopTimeout); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Stop(%v/%v) failed: %v", appID, instanceID, err))
	}
}

func suspendApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := appStub(appID, instanceID).Suspend(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Suspend(%v/%v) failed: %v", appID, instanceID, err))
	}
}

func resumeApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := appStub(appID, instanceID).Resume(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Resume(%v/%v) failed: %v", appID, instanceID, err))
	}
}

func resumeAppExpectError(t *testing.T, ctx *context.T, appID, instanceID string, expectedError verror.ID) {
	if err := appStub(appID, instanceID).Resume(ctx); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Resume(%v/%v) expected to fail with %v, got %v instead", appID, instanceID, expectedError, err))
	}
}

func updateApp(t *testing.T, ctx *context.T, appID string) {
	if err := appStub(appID).Update(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v) failed: %v", appID, err))
	}
}

func updateAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if err := appStub(appID).Update(ctx); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v) expected to fail with %v, got %v instead", appID, expectedError, err))
	}
}

func revertApp(t *testing.T, ctx *context.T, appID string) {
	if err := appStub(appID).Revert(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Revert(%v) failed: %v", appID, err))
	}
}

func revertAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if err := appStub(appID).Revert(ctx); err == nil || !verror2.Is(err, expectedError) {
		t.Fatalf(testutil.FormatLogLine(2, "Revert(%v) expected to fail with %v, got %v instead", appID, expectedError, err))
	}
}

func uninstallApp(t *testing.T, ctx *context.T, appID string) {
	if err := appStub(appID).Uninstall(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Uninstall(%v) failed: %v", appID, err))
	}
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
	output += "exec " + os.Args[0] + " -minuid=1 -test.run=TestSuidHelper $*"
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
