// Test the device manager and related services and tools.
//
// By default, this script tests the device manager in a fashion amenable
// to automatic testing: the --single_user is passed to the device
// manager so that all device manager components run as the same user and
// no user input (such as an agent pass phrase) is needed.
//
// When this script is invoked with the --with_suid <user> flag, it
// installs the device manager in its more secure multi-account
// configuration where the device manager runs under the account of the
// invoker and test apps will be executed as <user>. This mode will
// require root permisisons to install and may require configuring an
// agent passphrase.
//
// For exanple:
//
//   v23 go test -v . --v23.tests --with_suid vanaguest
//
// to test a device manager with multi-account support enabled for app
// account vanaguest.
//
package mgmt_test

//go:generate v23 test generate .

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/testutil/v23tests"
	_ "v.io/core/veyron/profiles"
)

var (
	suidUserFlag string
	hostname     string
	errTimeout   = errors.New("timeout")
)

func init() {
	flag.StringVar(&suidUserFlag, "with_suid", "", "run the device manager as the specified user")
	name, err := os.Hostname()
	if err != nil {
		panic(fmt.Sprintf("Hostname() failed: %v", err))
	}
	hostname = name
}

// waitFor waits for the specified timeout for the supplied function parameter
// function to return a result or an error. If error is nil, the function
// will be called again to see if a result can be obtained until the timeout
// expires.
func waitFor(fn func() (string, error), timeout time.Duration) (string, error) {
	errCh := make(chan error)
	resultCh := make(chan string)
	cancelCh := make(chan struct{})

	eval := func() {
		for {
			result, err := fn()
			switch {
			case err != nil:
				errCh <- err
				close(errCh)
				close(resultCh)
				return
			case len(result) > 0:
				resultCh <- result
				close(resultCh)
				close(errCh)
				return
			}
			time.Sleep(100 * time.Millisecond)
			select {
			case <-cancelCh:
				return
			default:
			}
		}
	}

	go eval()

	select {
	case <-time.After(timeout):
		close(cancelCh)
		return "", errTimeout
	case result := <-resultCh:
		return strings.TrimRight(result, "\n"), nil
	case err := <-errCh:
		return "", err
	}
}

func waitForOrDie(i *v23tests.T, fn func() (string, error), timeout time.Duration) string {
	result, err := waitFor(fn, timeout)
	if err != nil {
		i.Fatal(fmt.Sprintf("%s: %s", v23tests.Caller(1), err))
	}
	return result
}

func V23TestNodeManager(i *v23tests.T) {
	defer fmt.Fprintf(os.Stderr, "--------------- SHUTDOWN ---------------\n")
	userFlag := "--single_user"
	withSuid := false
	if len(suidUserFlag) > 0 {
		userFlag = "--with_suid=" + suidUserFlag
		withSuid = true
	}
	i.Logf("user flag: %q", userFlag)

	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")
	workDir := i.NewTempDir()

	mkSubdir := func(sub string) string {
		n := filepath.Join(workDir, sub)
		if err := os.Mkdir(n, 0755); err != nil {
			i.Fatalf("failed to create %q: %v", n, err)
		}
		return n
	}

	binStagingDir := mkSubdir("bin")
	agentServerBin := i.BuildGoPkg("v.io/core/veyron/security/agent/agentd")
	suidHelperBin := i.BuildGoPkg("v.io/core/veyron/services/mgmt/suidhelper")
	initHelperBin := i.BuildGoPkg("v.io/core/veyron/services/mgmt/inithelper")

	// Device manager and principal use their own set of credentials.
	// The credentials directory will be populated with Start an application
	// server under the blessing "alice/myworkstation/applicationd" so that
	// the device ("alice/myworkstation") can talk to it. ALl of the binaries
	// that communicate with each other must share this credentials directory.
	credentials := "VEYRON_CREDENTIALS=" + i.NewTempDir()
	namespaceBin := i.BuildGoPkg("v.io/core/veyron/tools/namespace").WithEnv(credentials)
	debugBin := i.BuildGoPkg("v.io/core/veyron/tools/debug").WithEnv(credentials)
	deviceBin := i.BuildGoPkg("v.io/core/veyron/tools/mgmt/device").WithEnv(credentials)
	devicedBin := i.BuildGoPkg("v.io/core/veyron/services/mgmt/device/deviced").WithEnv(credentials)
	deviceScript := i.BinaryFromPath("device/devicex").WithEnv(credentials)
	principalBin := i.BuildGoPkg("v.io/core/veyron/tools/principal").WithEnv(credentials)
	binarydBin := i.BuildGoPkg("v.io/core/veyron/services/mgmt/binary/binaryd").WithEnv(credentials)
	binaryBin := i.BuildGoPkg("v.io/core/veyron/tools/binary").WithEnv(credentials)
	applicationdBin := i.BuildGoPkg("v.io/core/veyron/services/mgmt/application/applicationd").WithEnv(credentials)
	applicationBin := i.BuildGoPkg("v.io/core/veyron/tools/application").WithEnv(credentials)

	appDName := "applicationd"
	devicedAppName := filepath.Join(appDName, "deviced", "test")

	i.BinaryFromPath("/bin/cp").Start(agentServerBin.Path(), suidHelperBin.Path(), initHelperBin.Path(), devicedBin.Path(), binStagingDir).WaitOrDie(os.Stdout, os.Stderr)

	dmInstallDir := filepath.Join(workDir, "dm")
	i.SetVar("VANADIUM_DEVICE_DIR", dmInstallDir)

	neighborhoodName := fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), rand.Int())

	deviceScript.Start(
		"install",
		binStagingDir,
		userFlag,
		"--origin="+devicedAppName,
		"--",
		"--veyron.tcp.address=127.0.0.1:0",
		"--neighborhood_name="+neighborhoodName).
		WaitOrDie(os.Stdout, os.Stderr)

	deviceScript.Start("start").WaitOrDie(os.Stdout, os.Stderr)

	mtName := "devices/" + hostname

	resolve := func(name string) (string, error) {
		inv := namespaceBin.Start("resolve", name)
		// Avoid leaving a ton of invocations lying around to obscure
		// logging output. Ignore the error code from Wait since it'll
		// deadlock if we call Fatalf etc from a goroutine.
		defer inv.Wait(nil, os.Stderr)
		return strings.TrimRight(inv.Output(), "\n"), nil
	}
	resolveMtName := func() (string, error) {
		return resolve(mtName)
	}

	mtEP := waitForOrDie(i, resolveMtName, time.Minute)

	run := func(bin *v23tests.Binary, args ...string) string {
		var stdout, stderr bytes.Buffer
		inv := bin.Start(args...)
		if err := inv.Wait(&stdout, &stderr); err != nil {
			loc := v23tests.Caller(1)
			i.Logf("%s: %v failed: %s", loc, filepath.Base(bin.Path()), err)
			i.Fatalf("%s: %v", loc, stderr.String())
		}
		return strings.TrimRight(stdout.String(), "\n")
	}

	// Verify that device manager's mounttable is published under the expected
	// name (hostname).
	if got := run(namespaceBin, "glob", mtName); len(got) == 0 {
		i.Fatalf("glob failed for %q", mtName)
	}

	// Create a self-signed blessing with name "alice" and set it as default
	// and shareable with all peers on the principal that the device manager
	// and principal are sharing (via the .WithEnv method) above. This
	// blessing will be used by all commands run by the device manager that
	// specify the same credentials.
	// TODO - update these commands
	// that except those
	// run with a different which gets a principal forked from the
	// process principal.
	blessingFilename := filepath.Join(workDir, "alice.bless")
	blessing := run(principalBin, "blessself", "alice")
	if err := ioutil.WriteFile(blessingFilename, []byte(blessing), 0755); err != nil {
		i.Fatal(err)
	}
	run(principalBin, "store", "setdefault", blessingFilename)
	run(principalBin, "store", "set", blessingFilename, "...")
	defer os.Remove(blessingFilename)

	// Claim the device as "alice/myworkstation".
	deviceBin.Start("claim", mtName+"/devmgr/device", "myworkstation")

	resolveChanged := func(name, prev string) (string, error) {
		ep, err := resolve(name)
		if err == nil && ep != prev {
			return ep, nil
		}
		return "", nil
	}

	waitForChange := func(name, prevEP string) string {
		newEP := waitForOrDie(i, func() (string, error) { return resolveChanged(name, prevEP) }, time.Minute)
		loc := v23tests.Caller(1)
		if newEP == prevEP {
			i.Fatalf("%s: failed to obtain new endpoint for %s: same as before %v", loc, name, newEP)
		}
		vlog.Infof("%s: new endpoint: %s -> %s", loc, name, newEP)
		return newEP
	}

	// Wait for the device manager to update its mount table entry.
	mtEP = waitForChange(mtName, mtEP)

	if withSuid {
		/*
		   		   "${DEVICE_BIN}" associate add "${MT_NAME}/devmgr/device" "${SUID_USER}"  "alice"
		        shell_test::assert_eq   "$("${DEVICE_BIN}" associate list "${MT_NAME}/devmgr/device")" \
		          "alice ${SUID_USER}" "${LINENO}"
		*/
	}

	// Verify the device's default blessing is as expected.
	inv := debugBin.Start("stats", "read", mtName+"/devmgr/__debug/stats/security/principal/*/blessingstore")
	inv.ExpectRE(".*Default blessings: alice/myworkstation$", -1)

	// Get the device's profile, which should be set to non-empty string
	inv = deviceBin.Start("describe", mtName+"/devmgr/device")

	parts := inv.ExpectRE(`{Profiles:map\[(.*):{}\]}`, 1)
	expectOneMatch := func(parts [][]string) string {
		if len(parts) != 1 || len(parts[0]) != 2 {
			loc := v23tests.Caller(1)
			i.Fatalf("%s: failed to match profile: %#v", loc, parts)
		}
		return parts[0][1]
	}
	deviceProfile := expectOneMatch(parts)
	if len(deviceProfile) == 0 {
		i.Fatalf("failed to get profile")
	}

	binarydName := "binaryd"
	// Start an application server under the blessing
	// "alice/myworkstation/applicationd" so that
	// the device ("alice/myworkstation") can talk to it.
	binarydBin.Start(
		"--name="+binarydName,
		"--root_dir="+filepath.Join(workDir, "binstore"),
		"--veyron.tcp.address=127.0.0.1:0",
		"--http=127.0.0.1:0")

	sampleAppBinName := binarydName + "/testapp"
	run(binaryBin, "upload", sampleAppBinName, binarydBin.Path())

	// Verify that the binary we uploaded is shown by glob, we need to run
	// with the same blessed credentials as binaryd in order to be able to
	// glob its names pace.
	if got := run(namespaceBin.WithEnv(credentials), "glob", sampleAppBinName); len(got) == 0 {
		i.Fatalf("glob failed for %q", sampleAppBinName)
	}

	appstoreDir := mkSubdir("apptstore")

	applicationdBin.Start(
		"--name="+appDName,
		"--store="+appstoreDir,
		"--veyron.tcp.address=127.0.0.1:0",
	)

	sampleAppName := appDName + "/testapp/v0"
	appPubName := "testbinaryd"
	appEnvelopeFilename := filepath.Join(workDir, "app.envelope")
	appEnvelope := fmt.Sprintf("{\"Title\":\"BINARYD\", \"Args\":[\"--name=%s\", \"--root_dir=./binstore\", \"--veyron.tcp.address=127.0.0.1:0\", \"--http=127.0.0.1:0\"], \"Binary\":{\"File\":%q}, \"Env\":[]}", appPubName, sampleAppBinName)
	ioutil.WriteFile(appEnvelopeFilename, []byte(appEnvelope), 0666)
	defer os.Remove(appEnvelopeFilename)

	output := run(applicationBin, "put", sampleAppName, deviceProfile, appEnvelopeFilename)
	if got, want := output, "Application envelope added successfully."; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// Verify that the envelope we uploaded shows up with glob.
	inv = applicationBin.Start("match", sampleAppName, deviceProfile)
	parts = inv.ExpectSetEventuallyRE(`"Title": "(.*)",`, `"File": "(.*)",`)
	if got, want := len(parts), 2; got != want {
		i.Fatalf("got %d, want %d", got, want)
	}
	for line, want := range []string{"BINARYD", sampleAppBinName} {
		if got := parts[line][1]; got != want {
			i.Fatalf("got %q, want %q", got, want)
		}
	}

	// Install the app on the device.
	inv = deviceBin.Start("install", mtName+"/devmgr/apps", sampleAppName)
	parts = inv.ExpectRE(`Successfully installed: "(.*)"`, 1)
	installationName := expectOneMatch(parts)

	// Verify that the installation shows up when globbing the device manager.
	output = run(namespaceBin, "glob", mtName+"/devmgr/apps/BINARYD/*")
	if got, want := output, installationName; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// Start an instance of the app, granting it blessing extension myapp.
	inv = deviceBin.Start("start", installationName, "myapp")
	parts = inv.ExpectRE(`Successfully started: "(.*)"`, 1)
	instanceName := expectOneMatch(parts)

	resolveInstanceName := func() (string, error) {
		return resolve(mtName + "/" + appPubName)
	}
	waitForOrDie(i, resolveInstanceName, time.Minute)

	// Verify that the instance shows up when globbing the device manager.
	output = run(namespaceBin, "glob", mtName+"/devmgr/apps/BINARYD/*/*")
	if got, want := output, instanceName; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// TODO(rjkroege): Verify that the app is actually running as ${SUID_USER}

	// Verify the app's default blessing.
	inv = debugBin.Start("stats", "read", instanceName+"/stats/security/principal/*/blessingstore")
	// Why is this alice/myworkstation/myapp/BINARYD and not
	// alice/myapp/BINARYD as seen by the test.sh?
	inv.ExpectRE(".*Default blessings: alice/myworkstation/myapp/BINARYD$", -1)

	// Stop the instance
	run(deviceBin, "stop", instanceName)

	// Verify that logs, but not stats, show up when globbing the
	// stopped instance.
	output = run(namespaceBin, "glob", instanceName+"/stats/...")
	if len(output) > 0 {
		i.Fatalf("no output expected for glob %s/stats/..., got %q", output, instanceName)
	}
	output = run(namespaceBin, "glob", instanceName+"/logs/...")
	if len(output) == 0 {
		i.Fatalf("output expected for glob %s/logs/..., but got none", instanceName)
	}

	// Upload a deviced binary
	devicedAppBinName := binarydName + "/deviced"
	run(binaryBin, "upload", devicedAppBinName, devicedBin.Path())

	// Upload a device manager envelope, make sure that we set
	// VEYRON_CREDENTIALS in the enevelope, otherwise the updated device
	// manager will use new credentials.
	devicedEnvelopeFilename := filepath.Join(workDir, "deviced.envelope")
	devicedEnvelope := fmt.Sprintf("{\"Title\":\"device manager\", \"Binary\":{\"File\":%q}, \"Env\":[%q]}", devicedAppBinName, credentials)
	ioutil.WriteFile(devicedEnvelopeFilename, []byte(devicedEnvelope), 0666)
	defer os.Remove(devicedEnvelopeFilename)
	run(applicationBin, "put", devicedAppName, deviceProfile, devicedEnvelopeFilename)

	// Update the device manager.
	run(deviceBin, "update", mtName+"/devmgr/device")
	mtEP = waitForChange(mtName, mtEP)

	// Verify that device manager's mounttable is still published under the
	// expected name (hostname).
	if run(namespaceBin, "glob", mtName) == "" {
		i.Fatalf("failed to glob %s", mtName)
	}

	// Revert the device manager
	run(deviceBin, "revert", mtName+"/devmgr/device")
	mtEP = waitForChange(mtName, mtEP)

	// Verify that device manager's mounttable is still published under the
	// expected name (hostname).
	if run(namespaceBin, "glob", mtName) == "" {
		i.Fatalf("failed to glob %s", mtName)
	}

	// Verify that the local mounttable exists, and that the device manager,
	// the global namespace, and the neighborhood are mounted on it.
	n := mtEP + "/devmgr"
	if run(namespaceBin, "resolve", n) == "" {
		i.Fatalf("failed to resolve %s", n)
	}
	n = mtEP + "/nh"
	if run(namespaceBin, "resolve", n) == "" {
		i.Fatalf("failed to resolve %s", n)
	}
	namespaceRoot, _ := i.GetVar("NAMESPACE_ROOT")
	n = mtEP + "/global"
	// TODO(ashankar): The expected blessings of the namespace root should
	// also be from some VAR or something.  For now, hardcoded, but this
	// should be fixed along with
	// https://github.com/veyron/release-issues/issues/98
	if got, want := run(namespaceBin, "resolve", n), fmt.Sprintf("[alice/myworkstation]%v", namespaceRoot); got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// Suspend the device manager.
	run(deviceBin, "suspend", mtName+"/devmgr/device")
	mtEP = waitForChange(mtName, mtEP)

	// Stop the device manager.
	run(deviceScript, "stop")

	// Wait for the mounttable entry to go away.
	noEntry := func() (string, error) {
		ep, err := resolve(mtName)
		if err == nil && len(ep) == 0 {
			return "done", nil
		}
		return "", nil
	}
	waitForOrDie(i, noEntry, time.Minute)

	fi, err := ioutil.ReadDir(dmInstallDir)
	if err != nil {
		i.Fatalf("failed to readdir for %q: %v", dmInstallDir, err)
	}

	run(deviceScript, "uninstall")

	fi, err = ioutil.ReadDir(dmInstallDir)
	if err == nil || len(fi) > 0 {
		i.Fatalf("managed to read %d entries from %q", len(fi), dmInstallDir)
	}
	if err != nil && !strings.Contains(err.Error(), "no such file or directory") {
		i.Fatalf("wrong error: %v", err)
	}
}
