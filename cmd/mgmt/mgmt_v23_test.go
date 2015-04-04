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
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"v.io/x/ref/envvar"
	_ "v.io/x/ref/profiles"
	"v.io/x/ref/test/v23tests"
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

func V23TestDeviceManager(i *v23tests.T) {
	defer fmt.Fprintf(os.Stderr, "--------------- SHUTDOWN ---------------\n")
	userFlag := "--single_user"
	withSuid := false
	if len(suidUserFlag) > 0 {
		userFlag = "--with_suid=" + suidUserFlag
		withSuid = true
	}
	i.Logf("user flag: %q", userFlag)

	var (
		workDir       = i.NewTempDir()
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
		deviceBin       = i.BuildV23Pkg("v.io/x/ref/cmd/mgmt/device").WithStartOpts(aliceOpts)
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
		deviceScript = i.BinaryFromPath("device/devicex").WithEnv("V23_DEVICE_DIR=" + dmInstallDir)

		mtName = "devices/" + hostname // Name under which the device manager will publish itself.
	)

	v23tests.RunRootMT(i, "--v23.tcp.address=127.0.0.1:0")
	buildAndCopyBinaries(
		i,
		binStagingDir,
		"v.io/x/ref/services/mgmt/device/deviced",
		"v.io/x/ref/security/agent/agentd",
		"v.io/x/ref/services/mgmt/suidhelper",
		"v.io/x/ref/services/mgmt/inithelper")

	appDName := "applicationd"
	devicedAppName := filepath.Join(appDName, "deviced", "test")

	deviceScript.Start(
		"install",
		binStagingDir,
		userFlag,
		"--origin="+devicedAppName,
		"--",
		"--v23.tcp.address=127.0.0.1:0",
		"--neighborhood-name="+fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), rand.Int())).
		WaitOrDie(os.Stdout, os.Stderr)
	deviceScript.Start("start").WaitOrDie(os.Stdout, os.Stderr)

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
	mtEP := resolve(mtName)

	// Verify that device manager's mounttable is published under the expected
	// name (hostname).
	if got := namespaceBin.Run("glob", mtName); len(got) == 0 {
		i.Fatalf("glob failed for %q", mtName)
	}

	// Claim the device as "root/alice/myworkstation".
	deviceBin.Start("claim", mtName+"/devmgr/device", "myworkstation")

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

	// Wait for the device manager to update its mount table entry.
	mtEP = resolveChange(mtName, mtEP)

	if withSuid {
		/*
		   		   "${DEVICE_BIN}" associate add "${MT_NAME}/devmgr/device" "${SUID_USER}"  "alice"
		        shell_test::assert_eq   "$("${DEVICE_BIN}" associate list "${MT_NAME}/devmgr/device")" \
		          "alice ${SUID_USER}" "${LINENO}"
		*/
	}

	// Verify the device's default blessing is as expected.
	inv := debugBin.Start("stats", "read", mtName+"/devmgr/__debug/stats/security/principal/*/blessingstore")
	inv.ExpectRE(".*Default blessings: root/alice/myworkstation$", -1)

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
	parts = inv.ExpectRE(`Successfully installed: "(.*)"`, 1)
	installationName := expectOneMatch(parts)

	// Verify that the installation shows up when globbing the device manager.
	output = namespaceBin.Run("glob", mtName+"/devmgr/apps/BINARYD/*")
	if got, want := output, installationName; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// Start an instance of the app, granting it blessing extension myapp.
	inv = deviceBin.Start("start", installationName, "myapp")
	parts = inv.ExpectRE(`Successfully started: "(.*)"`, 1)
	instanceName := expectOneMatch(parts)

	resolve(mtName + "/" + appPubName)

	// Verify that the instance shows up when globbing the device manager.
	output = namespaceBin.Run("glob", mtName+"/devmgr/apps/BINARYD/*/*")
	if got, want := output, instanceName; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}

	// TODO(rjkroege): Verify that the app is actually running as ${SUID_USER}

	// Verify the app's default blessing.
	inv = debugBin.Start("stats", "read", instanceName+"/stats/security/principal/*/blessingstore")
	inv.ExpectRE(".*Default blessings: root/alice/myapp$", -1)

	// Stop the instance
	deviceBin.Run("stop", instanceName)

	// Verify that logs, but not stats, show up when globbing the
	// stopped instance.
	if output = namespaceBin.Run("glob", instanceName+"/stats/..."); len(output) > 0 {
		i.Fatalf("no output expected for glob %s/stats/..., got %q", output, instanceName)
	}
	if output = namespaceBin.Run("glob", instanceName+"/logs/..."); len(output) == 0 {
		i.Fatalf("output expected for glob %s/logs/..., but got none", instanceName)
	}

	// Upload a deviced binary
	devicedAppBinName := binarydName + "/deviced"
	binaryBin.Run("upload", devicedAppBinName, i.BuildGoPkg("v.io/x/ref/services/mgmt/device/deviced").Path())

	// Upload a device manager envelope.
	devicedEnvelopeFilename := filepath.Join(workDir, "deviced.envelope")
	devicedEnvelope := fmt.Sprintf("{\"Title\":\"device manager\", \"Binary\":{\"File\":%q}}", devicedAppBinName)
	ioutil.WriteFile(devicedEnvelopeFilename, []byte(devicedEnvelope), 0666)
	defer os.Remove(devicedEnvelopeFilename)
	applicationBin.Run("put", devicedAppName, deviceProfile, devicedEnvelopeFilename)

	// Update the device manager.
	deviceBin.Run("update", mtName+"/devmgr/device")
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

	// Suspend the device manager, wait for the endpoint to change
	deviceBin.Run("suspend", mtName+"/devmgr/device")
	mtEP = resolveChange(mtName, mtEP)

	// Stop the device manager.
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

	fi, err := ioutil.ReadDir(dmInstallDir)
	if err != nil {
		i.Fatalf("failed to readdir for %q: %v", dmInstallDir, err)
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
