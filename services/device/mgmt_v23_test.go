// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test the device manager and related services and tools.
//
// By default, this script tests the device manager in a fashion amenable
// to automatic testing: the --single_user is passed to the device
// manager so that all device manager components run as the same user and
// no user input (such as an agent pass phrase) is needed.
//
// This script can exercise the device manager in two different modes. It
// can be executed like so:
//
// v23 go test -v . --v23.tests
//
// This will exercise the device manager's single user mode where all
// processes run as the same invoking user.
//
// Alternatively, the device manager can be executed in multiple account
// mode by providing the --deviceuser <deviceuser> and --appuser
// <appuser> flags. In this case, the device manager will run as user
// <devicemgr> and the test will run applications as user <appuser>. If
// executed in this fashion, root permissions will be required to install
// and it may require configuring an agent passphrase. For example:
//
//   v23 go test -v . --v23.tests --deviceuser devicemanager --appuser  vana
//
// NB: the accounts provided as arguments to this test must already exist.
// Also, the --v23.tests.shell-on-fail flag is useful to enable debugging
// output.

package device_test

//go:generate v23 test generate .

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"v.io/x/ref/envvar"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/v23tests"
)

var (
	appUserFlag    string
	deviceUserFlag string
	hostname       string
	errTimeout     = errors.New("timeout")
)

func init() {
	flag.StringVar(&appUserFlag, "appuser", "", "launch apps as the specified user")
	flag.StringVar(&deviceUserFlag, "deviceuser", "", "run the device manager as the specified user")
	name, err := os.Hostname()
	if err != nil {
		panic(fmt.Sprintf("Hostname() failed: %v", err))
	}
	hostname = name
}

func V23TestDeviceManager(i *v23tests.T) {
	u, err := user.Current()
	if err != nil {
		i.Fatalf("couldn't get the current user: %v", err)
	}
	testCore(i, u.Username, "", false)
}

func V23TestDeviceManagerMultiUser(i *v23tests.T) {
	u, err := user.Current()
	if err != nil {
		i.Fatalf("couldn't get the current user: %v", err)
	}

	if u.Username == "veyron" && runTestOnThisPlatform {
		// We are running on the builder so run the multiuser
		// test with default user names. These will be created as
		// required.
		makeTestAccounts(i)
		testCore(i, "vana", "devicemanager", true)
		return
	}

	if len(deviceUserFlag) > 0 && len(appUserFlag) > 0 {
		testCore(i, appUserFlag, deviceUserFlag, true)
	} else {
		i.Logf("Test skipped because running in multiuser mode requires --appuser and --deviceuser flags")
	}
}

func testCore(i *v23tests.T, appUser, deviceUser string, withSuid bool) {
	defer fmt.Fprintf(os.Stderr, "--------------- SHUTDOWN ---------------\n")
	userFlag := "--single_user"
	tempDir := ""

	if withSuid {
		// When running --with_suid, TMPDIR must grant the
		// invoking user rwx permissions and world x permissions for
		// all parent directories back to /. Otherwise, the
		// with_suid user will not be able to use absolute paths.
		// On Darwin, TMPDIR defaults to a directory hieararchy
		// in /var that is 0700. This is unworkable so force
		// TMPDIR to /tmp in this case.
		tempDir = "/tmp"
	}

	var (
		workDir       = i.NewTempDir(tempDir)
		binStagingDir = mkSubdir(i, workDir, "bin")
		dmInstallDir  = filepath.Join(workDir, "dm")

		// All vanadium command-line utitilities will be run by a
		// principal that has "root/alice" as its blessing.
		// (Where "root" comes from i.Principal().BlessingStore().Default()).
		// Create those credentials and options to use to setup the
		// binaries with them.
		aliceCreds, _ = i.Shell().NewChildCredentials("alice")
		aliceOpts     = i.Shell().DefaultStartOpts().ExternalCommand().WithCustomCredentials(aliceCreds)

		// Build all the command-line tools and set them up to run as alice.
		// applicationd/binaryd servers will be run by alice too.
		namespaceBin    = i.BuildV23Pkg("v.io/x/ref/cmd/namespace").WithStartOpts(aliceOpts)
		debugBin        = i.BuildV23Pkg("v.io/x/ref/services/debug/debug").WithStartOpts(aliceOpts)
		deviceBin       = i.BuildV23Pkg("v.io/x/ref/services/device/device").WithStartOpts(aliceOpts)
		binaryBin       = i.BuildV23Pkg("v.io/x/ref/services/binary/binary").WithStartOpts(aliceOpts)
		applicationBin  = i.BuildV23Pkg("v.io/x/ref/services/application/application").WithStartOpts(aliceOpts)
		binarydBin      = i.BuildV23Pkg("v.io/x/ref/services/binary/binaryd").WithStartOpts(aliceOpts)
		applicationdBin = i.BuildV23Pkg("v.io/x/ref/services/application/applicationd").WithStartOpts(aliceOpts)

		// The devicex script is not provided with any credentials, it
		// will generate its own.  This means that on "devicex start"
		// the device will have no useful credentials and until "device
		// claim" is invoked (as alice), it will just sit around
		// waiting to be claimed.
		//
		// Other binaries, like applicationd and binaryd will be run by alice.
		deviceScript = i.BinaryFromPath("./devicex").WithEnv("V23_DEVICE_DIR=" + dmInstallDir)

		mtName = "devices/" + hostname // Name under which the device manager will publish itself.
	)

	if withSuid {
		// In multiuser mode, deviceUserFlag needs execute access to
		// tempDir.
		if err := os.Chmod(workDir, 0711); err != nil {
			i.Fatalf("os.Chmod() failed: %v", err)
		}
	}

	v23tests.RunRootMT(i, "--v23.tcp.address=127.0.0.1:0")
	buildAndCopyBinaries(
		i,
		binStagingDir,
		"v.io/x/ref/services/device/deviced",
		"v.io/x/ref/services/agent/agentd",
		"v.io/x/ref/services/device/suidhelper",
		"v.io/x/ref/services/device/inithelper")

	appDName := "applicationd"
	devicedAppName := filepath.Join(appDName, "deviced", "test")

	deviceScriptArguments := []string{
		"install",
		binStagingDir,
	}

	if withSuid {
		deviceScriptArguments = append(deviceScriptArguments, "--devuser="+deviceUser)
	} else {
		deviceScriptArguments = append(deviceScriptArguments, userFlag)
	}

	deviceScriptArguments = append(deviceScriptArguments, []string{
		"--origin=" + devicedAppName,
		"--",
		"--v23.tcp.address=127.0.0.1:0",
		"--neighborhood-name=" + fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), rand.Int()),
	}...)

	deviceScript.Start(deviceScriptArguments...).WaitOrDie(os.Stdout, os.Stderr)
	deviceScript.Start("start").WaitOrDie(os.Stdout, os.Stderr)
	// Grab the endpoint for the claimable service from the device manager's
	// log.
	dmLog := filepath.Join(dmInstallDir, "dmroot/device-manager/logs/deviced.INFO")
	var claimableEP string
	expiry := time.Now().Add(30 * time.Second)
	for {
		if time.Now().After(expiry) {
			i.Fatalf("Timed out looking for claimable endpoint in %v", dmLog)
		}
		startLog, err := ioutil.ReadFile(dmLog)
		if err != nil {
			i.Logf("Couldn't read log %v: %v", dmLog, err)
			time.Sleep(time.Second)
			continue
		}
		re := regexp.MustCompile(`Unclaimed device manager \((.*)\)`)
		matches := re.FindSubmatch(startLog)
		if len(matches) == 0 {
			i.Logf("Couldn't find match in %v [%v]", dmLog, startLog)
			time.Sleep(time.Second)
			continue
		}
		if len(matches) != 2 {
			i.Fatalf("Wrong match in %v (%d) %v", dmLog, len(matches), string(matches[0]))
		}
		claimableEP = string(matches[1])
		break
	}
	// Claim the device as "root/alice/myworkstation".
	deviceBin.Start("claim", claimableEP, "myworkstation")

	resolve := func(name string) string {
		resolver := func() (interface{}, error) {
			// Use Start, rather than Run, since it's ok for 'namespace resolve'
			// to fail with 'name doesn't exist'
			inv := namespaceBin.Start("resolve", name)
			// Cleanup after ourselves to avoid leaving a ton of invocations
			// lying around which obscure logging output.
			defer inv.Wait(nil, os.Stderr)
			if r := strings.TrimRight(inv.Output(), "\n"); len(r) > 0 {
				return r, nil
			}
			return nil, nil
		}
		return i.WaitFor(resolver, 100*time.Millisecond, time.Minute).(string)
	}

	// Wait for the device manager to publish its mount table entry.
	mtEP := resolve(mtName)

	if withSuid {
		deviceBin.Start("associate", "add", mtName+"/devmgr/device", appUser, "root/alice")

		aai := deviceBin.Start("associate", "list", mtName+"/devmgr/device")
		if got, expected := strings.Trim(aai.Output(), "\n "), "root/alice "+appUser; got != expected {
			i.Fatalf("association test, got %v, expected %v", got, expected)
		}
	}

	// Verify the device's default blessing is as expected.
	inv := debugBin.Start("stats", "read", mtName+"/devmgr/__debug/stats/security/principal/*/blessingstore")
	inv.ExpectSetEventuallyRE(".*Default Blessings[ ]+root/alice/myworkstation$")

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

	// Start a binaryd server that will serve the binary for the test
	// application to be installed on the device.
	binarydName := "binaryd"
	binarydBin.Start(
		"--name="+binarydName,
		"--root-dir="+filepath.Join(workDir, "binstore"),
		"--v23.tcp.address=127.0.0.1:0",
		"--http=127.0.0.1:0")
	sampleAppBinName := binarydName + "/testapp"
	binaryBin.Run("upload", sampleAppBinName, binarydBin.Path())
	if got := namespaceBin.Run("glob", sampleAppBinName); len(got) == 0 {
		i.Fatalf("glob failed for %q", sampleAppBinName)
	}

	// Start an applicationd server that will serve the application
	// envelope for the test application to be installed on the device.
	applicationdBin.Start(
		"--name="+appDName,
		"--store="+mkSubdir(i, workDir, "appstore"),
		"--v23.tcp.address=127.0.0.1:0",
	)
	sampleAppName := appDName + "/testapp/v0"
	appPubName := "testbinaryd"
	appEnvelopeFilename := filepath.Join(workDir, "app.envelope")
	appEnvelope := fmt.Sprintf("{\"Title\":\"BINARYD\", \"Args\":[\"--name=%s\", \"--root-dir=./binstore\", \"--v23.tcp.address=127.0.0.1:0\", \"--http=127.0.0.1:0\"], \"Binary\":{\"File\":%q}, \"Env\":[]}", appPubName, sampleAppBinName)
	ioutil.WriteFile(appEnvelopeFilename, []byte(appEnvelope), 0666)
	defer os.Remove(appEnvelopeFilename)

	output := applicationBin.Run("put", sampleAppName, deviceProfile, appEnvelopeFilename)
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
	installationName := inv.ReadLine()
	if installationName == "" {
		i.Fatalf("got empty installation name from install")
	}

	// Verify that the installation shows up when globbing the device manager.
	output = namespaceBin.Run("glob", mtName+"/devmgr/apps/BINARYD/*")
	if got, want := output, installationName; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// Start an instance of the app, granting it blessing extension myapp.
	inv = deviceBin.Start("instantiate", installationName, "myapp")
	instanceName := inv.ReadLine()
	if instanceName == "" {
		i.Fatalf("got empty instance name from new")
	}
	deviceBin.Start("run", instanceName)

	resolve(mtName + "/" + appPubName)

	// Verify that the instance shows up when globbing the device manager.
	output = namespaceBin.Run("glob", mtName+"/devmgr/apps/BINARYD/*/*")
	if got, want := output, instanceName; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	inv = debugBin.Start("stats", "read", instanceName+"/stats/system/pid")
	pid := inv.ExpectRE("[0-9]+$", 1)[0][0]
	uname, err := getUserForPid(i, pid)
	if err != nil {
		i.Errorf("getUserForPid could not determine the user running pid %v", pid)
	}
	if uname != appUser {
		i.Errorf("app expected to be running as %v but is running as %v", appUser, uname)
	}

	// Verify the app's default blessing.
	inv = debugBin.Start("stats", "read", instanceName+"/stats/security/principal/*/blessingstore")
	inv.ExpectSetEventuallyRE(".*Default Blessings[ ]+root/alice/myapp$")

	// Kill and delete the instance.
	deviceBin.Run("kill", instanceName)
	deviceBin.Run("delete", instanceName)

	// Verify that logs, but not stats, show up when globbing the
	// not-running instance.
	if output = namespaceBin.Run("glob", instanceName+"/stats/..."); len(output) > 0 {
		i.Fatalf("no output expected for glob %s/stats/..., got %q", output, instanceName)
	}
	if output = namespaceBin.Run("glob", instanceName+"/logs/..."); len(output) == 0 {
		i.Fatalf("output expected for glob %s/logs/..., but got none", instanceName)
	}

	// Upload a deviced binary
	devicedAppBinName := binarydName + "/deviced"
	binaryBin.Run("upload", devicedAppBinName, i.BuildGoPkg("v.io/x/ref/services/device/deviced").Path())

	// Upload a device manager envelope.
	devicedEnvelopeFilename := filepath.Join(workDir, "deviced.envelope")
	devicedEnvelope := fmt.Sprintf("{\"Title\":\"device manager\", \"Binary\":{\"File\":%q}}", devicedAppBinName)
	ioutil.WriteFile(devicedEnvelopeFilename, []byte(devicedEnvelope), 0666)
	defer os.Remove(devicedEnvelopeFilename)
	applicationBin.Run("put", devicedAppName, deviceProfile, devicedEnvelopeFilename)

	// Update the device manager.
	deviceBin.Run("update", mtName+"/devmgr/device")
	resolveChange := func(name, old string) string {
		resolver := func() (interface{}, error) {
			inv := namespaceBin.Start("resolve", name)
			defer inv.Wait(nil, os.Stderr)
			if r := strings.TrimRight(inv.Output(), "\n"); len(r) > 0 && r != old {
				return r, nil
			}
			return nil, nil
		}
		return i.WaitFor(resolver, 100*time.Millisecond, time.Minute).(string)
	}
	mtEP = resolveChange(mtName, mtEP)

	// Verify that device manager's mounttable is still published under the
	// expected name (hostname).
	if namespaceBin.Run("glob", mtName) == "" {
		i.Fatalf("failed to glob %s", mtName)
	}

	// Revert the device manager
	deviceBin.Run("revert", mtName+"/devmgr/device")
	mtEP = resolveChange(mtName, mtEP)

	// Verify that device manager's mounttable is still published under the
	// expected name (hostname).
	if namespaceBin.Run("glob", mtName) == "" {
		i.Fatalf("failed to glob %s", mtName)
	}

	// Verify that the local mounttable exists, and that the device manager,
	// the global namespace, and the neighborhood are mounted on it.
	n := mtEP + "/devmgr"
	if namespaceBin.Run("resolve", n) == "" {
		i.Fatalf("failed to resolve %s", n)
	}
	n = mtEP + "/nh"
	if namespaceBin.Run("resolve", n) == "" {
		i.Fatalf("failed to resolve %s", n)
	}
	namespaceRoot, _ := i.GetVar(envvar.NamespacePrefix)
	n = mtEP + "/global"
	if got, want := namespaceBin.Run("resolve", n), namespaceRoot; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// Kill the device manager (which causes it to be restarted), wait for
	// the endpoint to change.
	deviceBin.Run("kill", mtName+"/devmgr/device")
	mtEP = resolveChange(mtName, mtEP)

	// Shut down the device manager.
	deviceScript.Run("stop")

	// Wait for the mounttable entry to go away.
	resolveGone := func(name string) string {
		resolver := func() (interface{}, error) {
			inv := namespaceBin.Start("resolve", name)
			defer inv.Wait(nil, os.Stderr)
			if r := strings.TrimRight(inv.Output(), "\n"); len(r) == 0 {
				return r, nil
			}
			return nil, nil
		}
		return i.WaitFor(resolver, 100*time.Millisecond, time.Minute).(string)
	}
	resolveGone(mtName)

	var fi []os.FileInfo

	// This doesn't work in multiuser mode because dmInstallDir is
	// owned by the device manager user and unreadable by the user
	// running this test.
	if !withSuid {
		fi, err = ioutil.ReadDir(dmInstallDir)
		if err != nil {
			i.Fatalf("failed to readdir for %q: %v", dmInstallDir, err)
		}
	}

	deviceScript.Run("uninstall")

	fi, err = ioutil.ReadDir(dmInstallDir)
	if err == nil || len(fi) > 0 {
		i.Fatalf("managed to read %d entries from %q", len(fi), dmInstallDir)
	}
	if err != nil && !strings.Contains(err.Error(), "no such file or directory") {
		i.Fatalf("wrong error: %v", err)
	}
}

func buildAndCopyBinaries(i *v23tests.T, destinationDir string, packages ...string) {
	var args []string
	for _, pkg := range packages {
		args = append(args, i.BuildGoPkg(pkg).Path())
	}
	args = append(args, destinationDir)
	i.BinaryFromPath("/bin/cp").Start(args...).WaitOrDie(os.Stdout, os.Stderr)
}

func mkSubdir(i *v23tests.T, parent, child string) string {
	dir := filepath.Join(parent, child)
	if err := os.Mkdir(dir, 0755); err != nil {
		i.Fatalf("failed to create %q: %v", dir, err)
	}
	return dir
}

var re = regexp.MustCompile("[ \t]+")

// getUserForPid determines the username running process pid.
func getUserForPid(i *v23tests.T, pid string) (string, error) {
	pidString := i.BinaryFromPath("/bin/ps").Start(psFlags).Output()
	for _, line := range strings.Split(pidString, "\n") {
		fields := re.Split(line, -1)
		if len(fields) > 1 && pid == fields[1] {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("Couldn't determine the user for pid %s", pid)
}
