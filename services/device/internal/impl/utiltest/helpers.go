// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package utiltest

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"v.io/x/lib/vlog"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/application"
	"v.io/v23/services/device"
	"v.io/v23/services/logreader"
	"v.io/v23/services/pprof"
	"v.io/v23/services/stats"
	"v.io/v23/verror"

	_ "v.io/x/ref/profiles/roaming"
	"v.io/x/ref/services/device/internal/impl"
	"v.io/x/ref/services/internal/servicetest"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

const (
	// TODO(caprita): Set the timeout in a more principled manner.
	killTimeout = 20 * time.Second
)

func EnvelopeFromShell(sh *modules.Shell, env []string, cmd, title string, args ...string) application.Envelope {
	args, nenv := sh.CommandEnvelope(cmd, env, args...)
	return application.Envelope{
		Title: title,
		Args:  args[1:],
		// TODO(caprita): revisit how the environment is sanitized for arbirary
		// apps.
		Env:    impl.VanadiumEnvironment(nenv),
		Binary: application.SignedFile{File: MockBinaryRepoName},
	}
}

// ResolveExpectNotFound verifies that the given name is not in the mounttable.
func ResolveExpectNotFound(t *testing.T, ctx *context.T, name string) {
	if me, err := v23.GetNamespace(ctx).Resolve(ctx, name); err == nil {
		t.Fatalf(testutil.FormatLogLine(2, "Resolve(%v) succeeded with results %v when it was expected to fail", name, me.Names()))
	} else if expectErr := naming.ErrNoSuchName.ID; verror.ErrorID(err) != expectErr {
		t.Fatalf(testutil.FormatLogLine(2, "Resolve(%v) failed with error %v, expected error ID %v", name, err, expectErr))
	}
}

// Resolve looks up the given name in the mounttable.
func Resolve(t *testing.T, ctx *context.T, name string, replicas int) []string {
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

func DeviceStub(name string) device.DeviceClientMethods {
	deviceName := naming.Join(name, "device")
	return device.DeviceClient(deviceName)
}

func ClaimDevice(t *testing.T, ctx *context.T, claimableName, deviceName, extension, pairingToken string) {
	// Setup blessings to be granted to the claimed device
	g := &granter{extension: extension}
	s := options.SkipServerEndpointAuthorization{}
	// Call the Claim RPC: Skip server authorization because the unclaimed
	// device presents nothing that can be used to recognize it.
	if err := device.ClaimableClient(claimableName).Claim(ctx, pairingToken, g, s); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Claim(%q) failed: %v [%v]", claimableName, pairingToken, verror.ErrorID(err), err))
	}
	// Wait for the device to remount itself with the device service after
	// being claimed.
	start := time.Now()
	for {
		_, err := v23.GetNamespace(ctx).Resolve(ctx, deviceName)
		if err == nil {
			return
		}
		vlog.VI(4).Infof("Resolve(%q) failed: %v", err)
		time.Sleep(time.Millisecond)
		if elapsed := time.Since(start); elapsed > time.Minute {
			t.Fatalf("Device hasn't remounted itself in %v since it was claimed", elapsed)
		}
	}
}

func ClaimDeviceExpectError(t *testing.T, ctx *context.T, name, extension, pairingToken string, errID verror.ID) {
	// Setup blessings to be granted to the claimed device
	g := &granter{extension: extension}
	s := options.SkipServerEndpointAuthorization{}
	// Call the Claim RPC
	if err := device.ClaimableClient(name).Claim(ctx, pairingToken, g, s); verror.ErrorID(err) != errID {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Claim(%q) expected to fail with %v, got %v [%v]", name, pairingToken, errID, verror.ErrorID(err), err))
	}
}

func UpdateDeviceExpectError(t *testing.T, ctx *context.T, name string, errID verror.ID) {
	if err := DeviceStub(name).Update(ctx); verror.ErrorID(err) != errID {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Update expected to fail with %v, got %v [%v]", name, errID, verror.ErrorID(err), err))
	}
}

func UpdateDevice(t *testing.T, ctx *context.T, name string) {
	if err := DeviceStub(name).Update(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Update() failed: %v [%v]", name, verror.ErrorID(err), err))
	}
}

func RevertDeviceExpectError(t *testing.T, ctx *context.T, name string, errID verror.ID) {
	if err := DeviceStub(name).Revert(ctx); verror.ErrorID(err) != errID {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Revert() expected to fail with %v, got %v [%v]", name, errID, verror.ErrorID(err), err))
	}
}

func RevertDevice(t *testing.T, ctx *context.T, name string) {
	if err := DeviceStub(name).Revert(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Revert() failed: %v [%v]", name, verror.ErrorID(err), err))
	}
}

func KillDevice(t *testing.T, ctx *context.T, name string) {
	if err := DeviceStub(name).Kill(ctx, killTimeout); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Kill(%v) failed: %v [%v]", name, killTimeout, verror.ErrorID(err), err))
	}
}

func ShutdownDevice(t *testing.T, ctx *context.T, name string) {
	if err := DeviceStub(name).Delete(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "%q.Delete() failed: %v [%v]", name, verror.ErrorID(err), err))
	}
}

// The following set of functions are convenience wrappers around various app
// management methods.

func Ocfg(opt []interface{}) device.Config {
	for _, o := range opt {
		if c, ok := o.(device.Config); ok {
			return c
		}
	}
	return device.Config{}
}

func Opkg(opt []interface{}) application.Packages {
	for _, o := range opt {
		if c, ok := o.(application.Packages); ok {
			return c
		}
	}
	return application.Packages{}
}

func AppStub(nameComponents ...string) device.ApplicationClientMethods {
	appsName := "dm/apps"
	appName := naming.Join(append([]string{appsName}, nameComponents...)...)
	return device.ApplicationClient(appName)
}

func StatsStub(nameComponents ...string) stats.StatsClientMethods {
	statsName := naming.Join(nameComponents...)
	return stats.StatsClient(statsName)
}

func InstallApp(t *testing.T, ctx *context.T, opt ...interface{}) string {
	appID, err := AppStub().Install(ctx, MockApplicationRepoName, Ocfg(opt), Opkg(opt))
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Install failed: %v [%v]", verror.ErrorID(err), err))
	}
	return appID
}

func InstallAppExpectError(t *testing.T, ctx *context.T, expectedError verror.ID, opt ...interface{}) {
	if _, err := AppStub().Install(ctx, MockApplicationRepoName, Ocfg(opt), Opkg(opt)); err == nil || verror.ErrorID(err) != expectedError {
		t.Fatalf(testutil.FormatLogLine(2, "Install expected to fail with %v, got %v [%v]", expectedError, verror.ErrorID(err), err))
	}
}

type granter struct {
	rpc.CallOpt
	p         security.Principal
	extension string
}

func (g *granter) Grant(ctx *context.T, call security.Call) (security.Blessings, error) {
	p := call.LocalPrincipal()
	return p.Bless(call.RemoteBlessings().PublicKey(), p.BlessingStore().Default(), g.extension, security.UnconstrainedUse())
}

func LaunchAppImpl(t *testing.T, ctx *context.T, appID, grant string) (string, error) {
	instanceID, err := NewInstanceImpl(t, ctx, appID, grant)
	if err != nil {
		return "", err
	}
	return instanceID, AppStub(appID, instanceID).Run(ctx)
}

func NewInstanceImpl(t *testing.T, ctx *context.T, appID, grant string) (string, error) {
	ctx, delete := context.WithCancel(ctx)
	defer delete()

	call, err := AppStub(appID).Instantiate(ctx)
	if err != nil {
		return "", err
	}
	for call.RecvStream().Advance() {
		switch msg := call.RecvStream().Value().(type) {
		case device.BlessServerMessageInstancePublicKey:
			p := v23.GetPrincipal(ctx)
			pubKey, err := security.UnmarshalPublicKey(msg.Value)
			if err != nil {
				return "", err
			}
			blessings, err := p.Bless(pubKey, p.BlessingStore().Default(), grant, security.UnconstrainedUse())
			if err != nil {
				return "", errors.New("bless failed")
			}
			call.SendStream().Send(device.BlessClientMessageAppBlessings{blessings})
		default:
			return "", fmt.Errorf("newInstanceImpl: received unexpected message: %#v", msg)
		}
	}
	var instanceID string
	if instanceID, err = call.Finish(); err != nil {
		return "", err
	}
	return instanceID, nil
}

func LaunchApp(t *testing.T, ctx *context.T, appID string) string {
	instanceID, err := LaunchAppImpl(t, ctx, appID, "forapp")
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "launching %v failed: %v [%v]", appID, verror.ErrorID(err), err))
	}
	return instanceID
}

func LaunchAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if _, err := LaunchAppImpl(t, ctx, appID, "forapp"); err == nil || verror.ErrorID(err) != expectedError {
		t.Fatalf(testutil.FormatLogLine(2, "launching %v expected to fail with %v, got %v [%v]", appID, expectedError, verror.ErrorID(err), err))
	}
}

func TerminateApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := AppStub(appID, instanceID).Kill(ctx, killTimeout); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Kill(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
	if err := AppStub(appID, instanceID).Delete(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Delete(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func KillApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := AppStub(appID, instanceID).Kill(ctx, killTimeout); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Kill(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func DeleteApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := AppStub(appID, instanceID).Delete(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Delete(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func RunApp(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := AppStub(appID, instanceID).Run(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Run(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func RunAppExpectError(t *testing.T, ctx *context.T, appID, instanceID string, expectedError verror.ID) {
	if err := AppStub(appID, instanceID).Run(ctx); err == nil || verror.ErrorID(err) != expectedError {
		t.Fatalf(testutil.FormatLogLine(2, "Run(%v/%v) expected to fail with %v, got %v [%v]", appID, instanceID, expectedError, verror.ErrorID(err), err))
	}
}

func UpdateInstance(t *testing.T, ctx *context.T, appID, instanceID string) {
	if err := AppStub(appID, instanceID).Update(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v/%v) failed: %v [%v]", appID, instanceID, verror.ErrorID(err), err))
	}
}

func UpdateInstanceExpectError(t *testing.T, ctx *context.T, appID, instanceID string, expectedError verror.ID) {
	if err := AppStub(appID, instanceID).Update(ctx); err == nil || verror.ErrorID(err) != expectedError {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v/%v) expected to fail with %v, got %v [%v]", appID, instanceID, expectedError, verror.ErrorID(err), err))
	}
}

func UpdateApp(t *testing.T, ctx *context.T, appID string) {
	if err := AppStub(appID).Update(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v) failed: %v [%v]", appID, verror.ErrorID(err), err))
	}
}

func UpdateAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if err := AppStub(appID).Update(ctx); err == nil || verror.ErrorID(err) != expectedError {
		t.Fatalf(testutil.FormatLogLine(2, "Update(%v) expected to fail with %v, got %v [%v]", appID, expectedError, verror.ErrorID(err), err))
	}
}

func RevertApp(t *testing.T, ctx *context.T, appID string) {
	if err := AppStub(appID).Revert(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Revert(%v) failed: %v [%v]", appID, verror.ErrorID(err), err))
	}
}

func RevertAppExpectError(t *testing.T, ctx *context.T, appID string, expectedError verror.ID) {
	if err := AppStub(appID).Revert(ctx); err == nil || verror.ErrorID(err) != expectedError {
		t.Fatalf(testutil.FormatLogLine(2, "Revert(%v) expected to fail with %v, got %v [%v]", appID, expectedError, verror.ErrorID(err), err))
	}
}

func UninstallApp(t *testing.T, ctx *context.T, appID string) {
	if err := AppStub(appID).Uninstall(ctx); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Uninstall(%v) failed: %v [%v]", appID, verror.ErrorID(err), err))
	}
}

func Debug(t *testing.T, ctx *context.T, nameComponents ...string) string {
	dbg, err := AppStub(nameComponents...).Debug(ctx)
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "Debug(%v) failed: %v [%v]", nameComponents, verror.ErrorID(err), err))
	}
	return dbg
}

func Status(t *testing.T, ctx *context.T, nameComponents ...string) device.Status {
	s, err := AppStub(nameComponents...).Status(ctx)
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(3, "Status(%v) failed: %v [%v]", nameComponents, verror.ErrorID(err), err))
	}
	return s
}

func VerifyState(t *testing.T, ctx *context.T, want interface{}, nameComponents ...string) string {
	s := Status(t, ctx, nameComponents...)
	var (
		state   interface{}
		version string
	)
	switch s := s.(type) {
	case device.StatusInstance:
		state = s.Value.State
		version = s.Value.Version
	case device.StatusInstallation:
		state = s.Value.State
		version = s.Value.Version
	default:
		t.Fatalf(testutil.FormatLogLine(2, "Status(%v) returned unknown type: %T", nameComponents, s))
	}
	if state != want {
		t.Fatalf(testutil.FormatLogLine(2, "Status(%v) state: wanted %v (%T), got %v (%T)", nameComponents, want, want, state, state))
	}
	return version
}

// Code to make Association lists sortable.
type byIdentity []device.Association

func (a byIdentity) Len() int           { return len(a) }
func (a byIdentity) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byIdentity) Less(i, j int) bool { return a[i].IdentityName < a[j].IdentityName }

func CompareAssociations(t *testing.T, got, expected []device.Association) {
	sort.Sort(byIdentity(got))
	sort.Sort(byIdentity(expected))
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("ListAssociations() got %v, expected %v", got, expected)
	}
}

// GenerateSuidHelperScript builds a script to execute the test target as
// a suidhelper instance and returns the path to the script.
func GenerateSuidHelperScript(t *testing.T, root string) string {
	output := "#!/bin/bash\n"
	output += "V23_SUIDHELPER_TEST=1"
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

// GenerateAgentScript creates a simple script that acts as the security agent
// for tests.  It blackholes arguments meant for the agent.
func GenerateAgentScript(t *testing.T, root string) string {
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

func CtxWithNewPrincipal(t *testing.T, ctx *context.T, idp *testutil.IDProvider, extension string) *context.T {
	ret, err := v23.WithPrincipal(ctx, testutil.NewPrincipal())
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "v23.WithPrincipal failed: %v", err))
	}
	if err := idp.Bless(v23.GetPrincipal(ret), extension); err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "idp.Bless(?, %q) failed: %v", extension, err))
	}
	return ret
}

// TODO(rjkroege): This helper is generally useful. Use it to reduce
// boiler plate across all device manager tests.
func StartupHelper(t *testing.T) (func(), *context.T, *modules.Shell, *application.Envelope, string, string, *testutil.IDProvider) {
	ctx, shutdown := test.InitForTest()
	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	// Make a new identity context.
	idp := testutil.NewIDProvider("root")
	ctx = CtxWithNewPrincipal(t, ctx, idp, "self")

	sh, deferFn := servicetest.CreateShellAndMountTable(t, ctx, nil)

	// Set up mock application and binary repositories.
	envelope, envCleanup := StartMockRepos(t, ctx)

	root, rootCleanup := servicetest.SetupRootDir(t, "devicemanager")
	if err := impl.SaveCreatorInfo(root); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := GenerateSuidHelperScript(t, root)

	return func() {
		rootCleanup()
		envCleanup()
		deferFn()
		shutdown()
	}, ctx, sh, envelope, root, helperPath, idp
}

type GlobTestVector struct {
	Name, Pattern string
	Expected      []string
}

type globTestRegexHelper struct {
	logFileTimeStampRE               *regexp.Regexp
	logFileTrimInfoRE                *regexp.Regexp
	logFileRemoveErrorFatalWarningRE *regexp.Regexp
	statsTrimRE                      *regexp.Regexp
}

func NewGlobTestRegexHelper(appName string) *globTestRegexHelper {
	return &globTestRegexHelper{
		logFileTimeStampRE:               regexp.MustCompile("(STDOUT|STDERR)-[0-9]+$"),
		logFileTrimInfoRE:                regexp.MustCompile(appName + `\..*\.INFO\.[0-9.-]+$`),
		logFileRemoveErrorFatalWarningRE: regexp.MustCompile("(ERROR|FATAL|WARNING)"),
		statsTrimRE:                      regexp.MustCompile("/stats/(rpc|system(/start-time.*)?)$"),
	}
}

// VerifyGlob verifies that for each GlobTestVector instance that the
// pattern returns the expected matches.
func VerifyGlob(t *testing.T, ctx *context.T, appName string, testcases []GlobTestVector, res *globTestRegexHelper) {
	for _, tc := range testcases {
		results, _, err := testutil.GlobName(ctx, tc.Name, tc.Pattern)
		if err != nil {
			t.Errorf(testutil.FormatLogLine(2, "unexpected glob error for (%q, %q): %v", tc.Name, tc.Pattern, err))
			continue
		}
		filteredResults := []string{}
		for _, name := range results {
			// Keep only the stats object names that match this RE.
			if strings.Contains(name, "/stats/") && !res.statsTrimRE.MatchString(name) {
				continue
			}
			// Remove ERROR, WARNING, FATAL log files because
			// they're not consistently there.
			if res.logFileRemoveErrorFatalWarningRE.MatchString(name) {
				continue
			}
			name = res.logFileTimeStampRE.ReplaceAllString(name, "$1-<timestamp>")
			name = res.logFileTrimInfoRE.ReplaceAllString(name, appName+".<*>.INFO.<timestamp>")
			filteredResults = append(filteredResults, name)
		}
		sort.Strings(filteredResults)
		sort.Strings(tc.Expected)
		if !reflect.DeepEqual(filteredResults, tc.Expected) {
			t.Errorf(testutil.FormatLogLine(2, "unexpected result for (%q, %q). Got %q, want %q", tc.Name, tc.Pattern, filteredResults, tc.Expected))
		}
	}
}

// VerifyFailGlob verifies that for each GlobTestVector instance that the
// pattern returns no matches.
func VerifyFailGlob(t *testing.T, ctx *context.T, testcases []GlobTestVector) {
	for _, tc := range testcases {
		results, _, _ := testutil.GlobName(ctx, tc.Name, tc.Pattern)
		if len(results) != 0 {
			t.Errorf(testutil.FormatLogLine(2, "verifyFailGlob should have failed for %q, %q", tc.Name, tc.Pattern))
		}
	}
}

// VerifyLog calls Size() on a selection of log file objects to
// demonstrate that the log files are accessible and have been written by
// the application.
func VerifyLog(t *testing.T, ctx *context.T, nameComponents ...string) {
	a := nameComponents
	pattern, prefix := a[len(a)-1], a[:len(a)-1]
	path := naming.Join(prefix...)
	files, _, err := testutil.GlobName(ctx, path, pattern)
	if err != nil {
		t.Errorf(testutil.FormatLogLine(2, "unexpected glob error: %v", err))
	}
	if want, got := 4, len(files); got < want {
		t.Errorf(testutil.FormatLogLine(2, "Unexpected number of matches. Got %d, want at least %d", got, want))
	}
	for _, file := range files {
		name := naming.Join(path, file)
		c := logreader.LogFileClient(name)
		if _, err := c.Size(ctx); err != nil {
			t.Errorf(testutil.FormatLogLine(2, "Size(%q) failed: %v", name, err))
		}
	}
}

// VerifyStatsValues call Value() on some of the stats objects to prove
// that they are correctly being proxied to the device manager.
func VerifyStatsValues(t *testing.T, ctx *context.T, nameComponents ...string) {
	a := nameComponents
	pattern, prefix := a[len(a)-1], a[:len(a)-1]
	path := naming.Join(prefix...)
	objects, _, err := testutil.GlobName(ctx, path, pattern)

	if err != nil {
		t.Errorf(testutil.FormatLogLine(2, "unexpected glob error: %v", err))
	}
	if want, got := 2, len(objects); got != want {
		t.Errorf(testutil.FormatLogLine(2, "Unexpected number of matches. Got %d, want %d", got, want))
	}
	for _, obj := range objects {
		name := naming.Join(path, obj)
		c := stats.StatsClient(name)
		if _, err := c.Value(ctx); err != nil {
			t.Errorf(testutil.FormatLogLine(2, "Value(%q) failed: %v", name, err))
		}
	}
}

// VerifyPProfCmdLine calls CmdLine() on the pprof object to validate
// that it the proxy correctly accessess pprof names.
func VerifyPProfCmdLine(t *testing.T, ctx *context.T, appName string, nameComponents ...string) {
	name := naming.Join(nameComponents...)
	c := pprof.PProfClient(name)
	v, err := c.CmdLine(ctx)
	if err != nil {
		t.Errorf(testutil.FormatLogLine(2, "CmdLine(%q) failed: %v", name, err))
	}
	if len(v) == 0 {
		t.Fatalf("Unexpected empty cmdline: %v", v)
	}
	if got, want := filepath.Base(v[0]), appName; got != want {
		t.Errorf(testutil.FormatLogLine(2, "Unexpected value for argv[0]. Got %v, want %v", got, want))
	}

}

func VerifyNoRunningProcesses(t *testing.T) {
	if impl.RunningChildrenProcesses() {
		t.Errorf("device manager incorrectly terminating with child processes still running")
	}
}

func SetNamespaceRootsForUnclaimedDevice(ctx *context.T) (*context.T, error) {
	origroots := v23.GetNamespace(ctx).Roots()
	roots := make([]string, len(origroots))
	for i, orig := range origroots {
		addr, suffix := naming.SplitAddressName(orig)
		origep, err := v23.NewEndpoint(addr)
		if err != nil {
			return nil, err
		}
		ep := naming.FormatEndpoint(
			origep.Addr().Network(),
			origep.Addr().String(),
			origep.RoutingID(),
			naming.ServesMountTable(origep.ServesMountTable()))
		roots[i] = naming.JoinAddressName(ep, suffix)
	}
	vlog.Infof("Changing namespace roots from %v to %v", origroots, roots)
	ctx, _, err := v23.WithNewNamespace(ctx, roots...)
	return ctx, err
}
