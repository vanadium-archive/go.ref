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
// jiri go test -v . --v23.tests
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
//   jiri go test -v . --v23.tests --deviceuser devicemanager --appuser  vana
//
// NB: the accounts provided as arguments to this test must already exist.
// Also, the --v23.tests.shell-on-fail flag is useful to enable debugging
// output. Note that this flag does not work for some shells. Set
// $SHELL in that case.

package device_test

//go:generate jiri test generate .

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"v.io/x/ref"
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

		// Most vanadium command-line utilities will be run by a
		// principal that has "root:u:alice" as its blessing.
		// (Where "root" comes from i.Principal().BlessingStore().Default()).
		// Create those credentials and options to use to setup the
		// binaries with them.
		aliceCreds, _ = i.Shell().NewChildCredentials("u:alice")
		aliceOpts     = i.Shell().DefaultStartOpts().ExternalProgram().WithCustomCredentials(aliceCreds)

		// Build all the command-line tools and set them up to run as alice.
		// applicationd/binaryd servers will be run by alice too.
		// TODO: applicationd/binaryd should run as a separate "service" role, as
		// alice is just a user.
		namespaceBin    = i.BuildV23Pkg("v.io/x/ref/cmd/namespace").WithStartOpts(aliceOpts)
		deviceBin       = i.BuildV23Pkg("v.io/x/ref/services/device/device").WithStartOpts(aliceOpts)
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

	// We also need some tools running with different sets of credentials...

	// Administration tasks will be performed with a blessing that represents a corporate
	// adminstrator (which is usually a role account)
	adminCreds, err := i.Shell().NewChildCredentials("r:admin")
	if err != nil {
		i.Fatalf("generating admin creds: %v", err)
	}
	adminOpts := i.Shell().DefaultStartOpts().ExternalProgram().WithCustomCredentials(adminCreds)
	adminDeviceBin := deviceBin.WithStartOpts(adminOpts)
	debugBin := i.BuildV23Pkg("v.io/x/ref/services/debug/debug").WithStartOpts(adminOpts)

	// A special set of credentials will be used to give two blessings to the device manager
	// when claiming it -- one blessing will be from the corporate administrator role who owns
	// the machine, and the other will be a manufacturer blessing. (This is a hack until
	// there's a way to separately supply a manufacturer blessing. Eventually, the claim
	// would really be done by the administator, and the adminstrator's blessing would get
	// added to the manufacturer's blessing, which would already be present.)
	claimCreds, err := i.Shell().AddToChildCredentials(adminCreds, "m:orange:zphone5:ime-i007")
	if err != nil {
		i.Fatalf("adding the mfr blessing to admin creds: %v", err)
	}
	claimOpts := i.Shell().DefaultStartOpts().ExternalProgram().WithCustomCredentials(claimCreds)
	claimDeviceBin := deviceBin.WithStartOpts(claimOpts)

	// Another set of credentials be used to represent the application publisher, who
	// signs and pushes binaries
	pubCreds, err := i.Shell().NewChildCredentials("a:rovio")
	if err != nil {
		i.Fatalf("generating publisher creds: %v", err)
	}
	pubOpts := i.Shell().DefaultStartOpts().ExternalProgram().WithCustomCredentials(pubCreds)
	pubDeviceBin := deviceBin.WithStartOpts(pubOpts)
	applicationBin := i.BuildV23Pkg("v.io/x/ref/services/application/application").WithStartOpts(pubOpts)
	binaryBin := i.BuildV23Pkg("v.io/x/ref/services/binary/binary").WithStartOpts(pubOpts)

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

	appDName := "applications"
	devicedAppName := filepath.Join(appDName, "deviced", "test")

	deviceScriptArguments := []string{
		"install",
		binStagingDir,
	}

	if withSuid {
		deviceScriptArguments = append(deviceScriptArguments, "--devuser="+deviceUser)
	} else {
		deviceScriptArguments = append(deviceScriptArguments, "--single_user")
	}

	deviceScriptArguments = append(deviceScriptArguments, []string{
		"--origin=" + devicedAppName,
		"--",
		"--v23.tcp.address=127.0.0.1:0",
		"--neighborhood-name=" + fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), rand.Int()),
	}...)

	deviceScript.Start(deviceScriptArguments...).WaitOrDie(os.Stdout, os.Stderr)
	deviceScript.Start("start").WaitOrDie(os.Stdout, os.Stderr)
	dmLog := filepath.Join(dmInstallDir, "dmroot/device-manager/logs/deviced.INFO")
	stopDevMgr := func() {
		deviceScript.Run("stop")
		if dmLogF, err := os.Open(dmLog); err != nil {
			i.Errorf("Failed to read dm log: %v", err)
		} else {
			fmt.Fprintf(os.Stderr, "--------------- START DM LOG ---------------\n")
			defer dmLogF.Close()
			if _, err := io.Copy(os.Stderr, dmLogF); err != nil {
				i.Errorf("Error dumping dm log: %v", err)
			}
			fmt.Fprintf(os.Stderr, "--------------- END DM LOG ---------------\n")
		}
	}
	var stopDevMgrOnce sync.Once
	defer stopDevMgrOnce.Do(stopDevMgr)
	// Grab the endpoint for the claimable service from the device manager's
	// log.
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
		re := regexp.MustCompile(`Unclaimed device manager endpoint: (.*)`)
		matches := re.FindSubmatch(startLog)
		if len(matches) == 0 {
			i.Logf("Couldn't find match in %v [%s]", dmLog, startLog)
			time.Sleep(time.Second)
			continue
		}
		if len(matches) < 2 {
			i.Fatalf("Wrong match in %v (%d) %v", dmLog, len(matches), string(matches[0]))
		}
		claimableEP = string(matches[len(matches)-1])
		break
	}
	// Claim the device as "root:u:alice:myworkstation".
	claimDeviceBin.Start("claim", claimableEP, "myworkstation").WaitOrDie(os.Stdout, os.Stderr)

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
	adminDeviceBin.Run("acl", "set", mtName+"/devmgr/device", "root:u:alice", "Read,Resolve,Write")

	if withSuid {
		adminDeviceBin.Run("associate", "add", mtName+"/devmgr/device", appUser, "root:u:alice")
		associations := adminDeviceBin.Run("associate", "list", mtName+"/devmgr/device")
		if got, expected := strings.Trim(associations, "\n "), "root:u:alice "+appUser; got != expected {
			i.Fatalf("association test, got %v, expected %v", got, expected)
		}
	}

	// Verify the device's default blessing is as expected.
	mfrBlessing := "root:m:orange:zphone5:ime-i007:myworkstation"
	ownerBlessing := "root:r:admin:myworkstation"
	inv := debugBin.Start("stats", "read", mtName+"/devmgr/__debug/stats/security/principal/*/blessingstore/*")
	inv.ExpectSetEventuallyRE(".*Default Blessings[ ]+" + mfrBlessing + "," + ownerBlessing)
	inv.WaitOrDie(nil, os.Stderr)

	// Get the device's profile, which should be set to non-empty string
	inv = adminDeviceBin.Start("describe", mtName+"/devmgr/device")
	parts := inv.ExpectRE(`{Profiles:map\[(.*):{}\]}`, 1)
	inv.WaitOrDie(nil, os.Stderr)
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
	binarydName := "binaries"
	binarydBin.Start(
		"--name="+binarydName,
		"--root-dir="+filepath.Join(workDir, "binstore"),
		"--v23.tcp.address=127.0.0.1:0",
		"--http=127.0.0.1:0")
	// Allow publishers to update binaries
	deviceBin.Run("acl", "set", binarydName, "root:a", "Write")

	// We are also going to use the binaryd binary as our test app binary. Once our test app
	// binary is published to the binaryd server started above, this (augmented with a
	// timestamp) is the name the test app binary will have.
	sampleAppBinName := binarydName + "/binaryd"

	// Start an applicationd server that will serve the application
	// envelope for the test application to be installed on the device.
	applicationdBin.Start(
		"--name="+appDName,
		"--store="+mkSubdir(i, workDir, "appstore"),
		"--v23.tcp.address=127.0.0.1:0",
	)
	// Allow publishers to create and update envelopes
	deviceBin.Run("acl", "set", appDName, "root:a", "Read,Write,Resolve")

	sampleAppName := appDName + "/testapp"
	appPubName := "testbinaryd"
	appEnvelopeFilename := filepath.Join(workDir, "app.envelope")
	appEnvelope := fmt.Sprintf("{\"Title\":\"BINARYD\", \"Args\":[\"--name=%s\", \"--root-dir=./binstore\", \"--v23.tcp.address=127.0.0.1:0\", \"--http=127.0.0.1:0\"], \"Binary\":{\"File\":%q}, \"Env\":[]}", appPubName, sampleAppBinName)
	ioutil.WriteFile(appEnvelopeFilename, []byte(appEnvelope), 0666)
	defer os.Remove(appEnvelopeFilename)

	output := applicationBin.Run("put", sampleAppName+"/0", deviceProfile, appEnvelopeFilename)
	if got, want := output, fmt.Sprintf("Application envelope added for profile %s.", deviceProfile); got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// Verify that the envelope we uploaded shows up with glob.
	inv = applicationBin.Start("match", sampleAppName, deviceProfile)
	parts = inv.ExpectSetEventuallyRE(`"Title": "(.*)",`, `"File": "(.*)",`)
	inv.WaitOrDie(os.Stdout, os.Stderr)
	if got, want := len(parts), 2; got != want {
		i.Fatalf("got %d, want %d", got, want)
	}
	for line, want := range []string{"BINARYD", sampleAppBinName} {
		if got := parts[line][1]; got != want {
			i.Fatalf("got %q, want %q", got, want)
		}
	}

	// Publish the app (This uses the binarydBin binary and the testapp envelope from above)
	pubDeviceBin.Start("publish", "-from", filepath.Dir(binarydBin.Path()), "-readers", "root:r:admin", filepath.Base(binarydBin.Path())+":testapp").WaitOrDie(os.Stdout, os.Stderr)
	if got := namespaceBin.Run("glob", sampleAppBinName); len(got) == 0 {
		i.Fatalf("glob failed for %q", sampleAppBinName)
	}

	// Install the app on the device.
	inv = deviceBin.Start("install", mtName+"/devmgr/apps", sampleAppName)
	installationName := inv.ReadLine()
	inv.WaitOrDie(os.Stdout, os.Stderr)
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
	inv.WaitOrDie(os.Stdout, os.Stderr)
	if instanceName == "" {
		i.Fatalf("got empty instance name from new")
	}
	deviceBin.Start("run", instanceName).WaitOrDie(os.Stdout, os.Stderr)

	resolve(mtName + "/" + appPubName)

	// Verify that the instance shows up when globbing the device manager.
	output = namespaceBin.Run("glob", mtName+"/devmgr/apps/BINARYD/*/*")
	if got, want := output, instanceName; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	inv = debugBin.Start("stats", "read", instanceName+"/stats/system/pid")
	pid := inv.ExpectRE("[0-9]+$", 1)[0][0]
	inv.WaitOrDie(nil, os.Stderr)
	uname, err := getUserForPid(i, pid)
	if err != nil {
		i.Errorf("getUserForPid could not determine the user running pid %v", pid)
	} else if uname != appUser {
		i.Errorf("app expected to be running as %v but is running as %v", appUser, uname)
	}

	// Verify the app's blessings. We check the default blessing, as well as the
	// "..." blessing, which should be the default blessing plus a publisher blessing.
	userBlessing := "root:u:alice:myapp"
	pubBlessing := "root:a:rovio:apps:published:binaryd"
	appBlessing := mfrBlessing + ":a:" + pubBlessing + "," + ownerBlessing + ":a:" + pubBlessing
	inv = debugBin.Start("stats", "read", instanceName+"/stats/security/principal/*/blessingstore/*")
	inv.ExpectSetEventuallyRE(".*Default Blessings[ ]+"+userBlessing+"$", "[.][.][.][ ]+"+userBlessing+","+appBlessing)
	inv.WaitOrDie(nil, os.Stderr)

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

	// TODO: The deviced binary should probably be published by someone other than rovio :-)
	// Maybe publishing the deviced binary should eventually use "device publish" too?
	// For now, it uses the "application" and "binary" tools directly to ensure that those work

	// Upload a deviced binary
	devicedAppBinName := binarydName + "/deviced"
	binaryBin.Run("upload", devicedAppBinName, i.BuildGoPkg("v.io/x/ref/services/device/deviced").Path())
	// Allow root:r:admin and its devices to read the binary
	deviceBin.Run("acl", "set", devicedAppBinName, "root:r:admin", "Read")

	// Upload a device manager envelope.
	devicedEnvelopeFilename := filepath.Join(workDir, "deviced.envelope")
	devicedEnvelope := fmt.Sprintf("{\"Title\":\"device manager\", \"Binary\":{\"File\":%q}}", devicedAppBinName)
	ioutil.WriteFile(devicedEnvelopeFilename, []byte(devicedEnvelope), 0666)
	defer os.Remove(devicedEnvelopeFilename)
	applicationBin.Run("put", devicedAppName, deviceProfile, devicedEnvelopeFilename)
	// Allow root:r:admin and its devices to read the envelope
	deviceBin.Run("acl", "set", devicedAppName, "root:r:admin", "Read")

	// Update the device manager.
	adminDeviceBin.Run("update", mtName+"/devmgr/device")
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
	// The argument to "device revert" is a glob pattern. So we need to
	// wait for devmgr to be mounted before running the command.
	resolve(mtEP + "/devmgr")
	adminDeviceBin.Run("revert", mtName+"/devmgr/device")
	mtEP = resolveChange(mtName, mtEP)

	// Verify that device manager's mounttable is still published under the
	// expected name (hostname).
	if namespaceBin.Run("glob", mtName) == "" {
		i.Fatalf("failed to glob %s", mtName)
	}

	// Verify that the local mounttable exists, and that the device manager,
	// the global namespace, and the neighborhood are mounted on it.
	resolve(mtEP + "/devmgr")
	resolve(mtEP + "/nh")
	resolve(mtEP + "/global")

	namespaceRoot, _ := i.GetVar(ref.EnvNamespacePrefix)
	if got, want := namespaceBin.Run("resolve", mtEP+"/global"), namespaceRoot; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// Kill the device manager (which causes it to be restarted), wait for
	// the endpoint to change.
	deviceBin.Run("kill", mtName+"/devmgr/device")
	mtEP = resolveChange(mtName, mtEP)

	// Shut down the device manager.
	stopDevMgrOnce.Do(stopDevMgr)

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
	pidString := i.BinaryFromPath("/bin/ps").Run(psFlags)
	for _, line := range strings.Split(pidString, "\n") {
		fields := re.Split(line, -1)
		if len(fields) > 1 && pid == fields[1] {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("Couldn't determine the user for pid %s", pid)
}
