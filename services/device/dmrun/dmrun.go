// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command dmrun runs a binary on a remote VM instance using device manager.
//
// dmrun creates the VM instance, installs and starts device manager on it, and
// then installs and starts an app from the specified binary.
//
// dmrun uses the credentials it is running with in order to claim the device
// manager and provide the app with blessings.  To specify credentials for
// dmrun, use the V23_CREDENTIALS environment variable instead of the
// --v23.credentials flag.
//
// Usage:
//   dmrun [ENV=VAL ...] path/to/binary [--flag=val ...]
//
// All flags and environment variable settings are passed to the app.
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"v.io/x/ref"
	"v.io/x/ref/services/device/dmrun/backend"
)

var (
	workDir        string
	vcloud         string
	device         string
	vm             backend.CloudVM
	cleanupOnDeath func()
)

var dmBins = [...]string{
	"v.io/x/ref/services/device/deviced",
	"v.io/x/ref/services/agent/agentd",
	"v.io/x/ref/services/device/inithelper",
	"v.io/x/ref/services/device/suidhelper",
}

const (
	vcloudBin   = "v.io/x/devtools/vcloud"
	deviceBin   = "v.io/x/ref/services/device/device"
	devicexRepo = "release.go.x.ref"
	devicex     = "services/device/devicex"
)

func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	fmt.Fprintln(os.Stderr)
	if cleanupOnDeath != nil {
		savedCleanupFn := cleanupOnDeath
		cleanupOnDeath = func() {
			fmt.Fprintf(os.Stderr, "Avoided recursive call to cleanup in die()\n")
		}
		savedCleanupFn()
	}
	os.Exit(1)
}

func dieIfErr(err error, format string, args ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Encountered error: %v\n", err)
		die(format, args...)
	}
}

// getPath returns the filesystem path to a file specified by a repository and a
// repository-relative path.
func getPath(repo, file string) string {
	cmd := exec.Command("v23", "project", "list")
	output, err := cmd.CombinedOutput()
	out := string(output)
	dieIfErr(err, "Running %v failed. Output:\n%v", strings.Join(cmd.Args, " "), out)
	var projectPathRE = regexp.MustCompile(fmt.Sprintf("project=\"%s\" path=\"(.+)\"", repo))
	matches := projectPathRE.FindStringSubmatch(out)
	if matches == nil {
		die("Couldn't extract project path from %s", out)
	}
	return filepath.Join(matches[1], filepath.FromSlash(file))
}

// setupWorkDir creates a directory for all the local files created by this
// tool.
func setupWorkDir() {
	var err error
	workDir, err = ioutil.TempDir("", filepath.Base(os.Args[0]))
	dieIfErr(err, "Couldn't set up work dir")
	dieIfErr(os.Chmod(workDir, 0777), "Couldn't chmod work dir")
	fmt.Println("Working dir: %s", workDir)
}

// buildV23Binary builds the specified binary and returns the path to the
// executable.
func buildV23Binary(pkg string) string {
	fmt.Println("Building", pkg)
	dest := filepath.Join(workDir, path.Base(pkg))
	cmd := exec.Command("v23", "go", "build", "-x", "-o", dest, pkg)
	output, err := cmd.CombinedOutput()
	dieIfErr(err, "Running build command %v failed. Output:\n%v", strings.Join(cmd.Args, " "), string(output))
	return dest
}

// buildDMBinaries builds the binaries required for a device manager
// installation and returns the paths to the executables.
func buildDMBinaries() (ret []string) {
	for _, b := range dmBins {
		ret = append(ret, buildV23Binary(b))
	}
	return
}

// createArchive creates a zip archive from the given files.
func createArchive(files []string) string {
	zipFile := filepath.Join(workDir, "dm.zip")
	z, err := os.OpenFile(zipFile, os.O_CREATE|os.O_WRONLY, os.FileMode(0644))
	dieIfErr(err, "Couldn't create zip archive file")
	defer z.Close()
	w := zip.NewWriter(z)
	for _, file := range files {
		info, err := os.Stat(file)
		dieIfErr(err, "Couldn't stat %v", file)
		fh, err := zip.FileInfoHeader(info)
		dieIfErr(err, "Couldn't set up file info header")
		fh.Method = zip.Deflate
		fwrite, err := w.CreateHeader(fh)
		dieIfErr(err, "Couldn't create writer")
		fread, err := os.Open(file)
		dieIfErr(err, "Couldn't creater reader")
		_, err = io.Copy(fwrite, fread)
		dieIfErr(err, "Couldn't write to archive")
		dieIfErr(fread.Close(), "Couldn't close reader")
	}
	dieIfErr(w.Close(), "Couldn't close zip archive")
	return zipFile
}

// setupInstance creates a new VM instance and returns its name and IP address.
func setupInstance(vmOptions interface{}) (backend.CloudVM, string, string) {
	currUser, err := user.Current()
	dieIfErr(err, "Couldn't obtain current user")
	instanceName := fmt.Sprintf("%s-%s", currUser.Username, time.Now().UTC().Format("20060102-150405"))
	vm, err = backend.CreateCloudVM(instanceName, vmOptions)
	dieIfErr(err, "VM Instance Creation Failed: %v", err)
	instanceIP := vm.IP()
	// Install unzip so we can unpack the archive.
	// TODO(caprita): Use tar instead.
	output, err := vm.RunCommand("sudo", "apt-get", "install", "unzip")
	dieIfErr(err, "Installing unzip failed. Output:\n%v", string(output))
	fmt.Println("Created VM instance", instanceName, "with IP", instanceIP)
	return vm, instanceName, instanceIP
}

// installArchive ships the archive to the VM instance and unpacks it.
func installArchive(archive, instance string) {
	err := vm.CopyFile(archive, "/tmp/")
	dieIfErr(err, "Copying archive failed: %v", err)
	output, err := vm.RunCommand("unzip", path.Join("/tmp", filepath.Base(archive)), "-d", "/tmp/unpacked")
	dieIfErr(err, "Extracting archive failed. Output:\n%v", string(output))
}

// installDevice installs and starts device manager, and returns the public key
// and pairing token needed for claiming.
func installDevice(instance string) (string, string) {
	fmt.Println("Installing device manager...")
	defer fmt.Println("Done installing device manager...")
	output, err := vm.RunCommand("V23_DEVICE_DIR=/tmp/dm", "/tmp/unpacked/devicex", "install", "/tmp/unpacked", "--single_user", "--", "--v23.tcp.address=:8151", "--deviced-port=8150", "--proxy-port=8160", "--use-pairing-token")
	dieIfErr(err, "Installing device manager failed. Output:\n%v", string(output))
	output, err = vm.RunCommand("V23_DEVICE_DIR=/tmp/dm", "/tmp/unpacked/devicex", "start")
	dieIfErr(err, "Starting device manager failed. Output:\n%v", string(output))
	// Grab the token and public key from the device manager log.
	dieAfter := time.After(5 * time.Second)
	firstIteration := true
	for {
		if !firstIteration {
			select {
			case <-dieAfter:
				die("Failed to find token and public key in log: %v", string(output))
			case <-time.After(100 * time.Millisecond):
			}
		} else {
			firstIteration = false
		}
		output, err = vm.RunCommand("cat", "/tmp/dm/dmroot/device-manager/logs/deviced.INFO")
		dieIfErr(err, "Reading device manager log failed. Output:\n%v", string(output))
		pairingTokenRE := regexp.MustCompile("Device manager pairing token: (.*)")
		matches := pairingTokenRE.FindSubmatch(output)
		if matches == nil {
			continue
		}
		pairingToken := string(matches[1])
		publicKeyRE := regexp.MustCompile("public_key: (.*)")
		matches = publicKeyRE.FindSubmatch(output)
		if matches == nil {
			continue
		}
		publicKey := string(matches[1])
		return publicKey, pairingToken
	}
}

// setCredentialsEnv sets the command's environment to share the principal of
// dmrun.
func setCredentialsEnv(cmd *exec.Cmd) {
	// TODO(caprita): This doesn't work with --v23.credentials.
	if creds := os.Getenv(ref.EnvCredentials); len(creds) > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", ref.EnvCredentials, creds))
	} else if agentCreds := os.Getenv(ref.EnvAgentEndpoint); len(agentCreds) > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", ref.EnvAgentEndpoint, agentCreds))
	} else if agentCreds := os.Getenv(ref.EnvAgentPath); len(agentCreds) > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", ref.EnvAgentPath, agentCreds))
	} else {
		fmt.Fprintf(os.Stderr, "WARNING: no credentials found. You'll probably have authorization issues later on.\n")
	}
}

// claimDevice claims the device manager, blessing it with extension.
func claimDevice(deviceName, ip, publicKey, pairingToken, extension string) {
	fmt.Println("claiming device manager ...")
	cmd := exec.Command(device, "claim", deviceName, extension, pairingToken, publicKey)
	setCredentialsEnv(cmd)
	output, err := cmd.CombinedOutput()
	dieIfErr(err, "Claiming device manager (%v) failed. Output:\n%v", strings.Join(cmd.Args, " "), string(output))
	cmd = exec.Command(device, "acl", "get", fmt.Sprintf("/%s:8151/devmgr/device", ip))
	setCredentialsEnv(cmd)
	output, err = cmd.CombinedOutput()
	dieIfErr(err, "Getting device manager acls (%v) failed. Output:\n%v", strings.Join(cmd.Args, " "), string(output))
	fmt.Printf("Done claiming device manager. Device manager ACLs:\n%s", string(output))
}

// installApp installs the binary specified on the command-line and returns the
// Vanadium name for the installation object.
func installApp(deviceName, ip string) string {
	args := []string{fmt.Sprintf("--v23.proxy=/%s:8160", ip), "install-local", deviceName + "/apps", "app"}
	args = append(args, flag.Args()...)
	cmd := exec.Command(device, args...)
	setCredentialsEnv(cmd)
	cmd.Env = append(cmd.Env, fmt.Sprintf("V23_NAMESPACE=/%s:8151", ip))
	output, err := cmd.CombinedOutput()
	dieIfErr(err, "Installing app (%v) failed. Output:\n%v", strings.Join(cmd.Args, " "), string(output))
	installationName := strings.TrimSpace(string(output))
	fmt.Println("Installed", installationName)
	return installationName
}

// startApp creates and launches an instance of the given installation, blessing
// it with extension.  It returns the Vanadium name for the instance object.
func startApp(installationName, extension string) string {
	cmd := exec.Command(device, "instantiate", installationName, extension)
	setCredentialsEnv(cmd)
	output, err := cmd.CombinedOutput()
	dieIfErr(err, "Instantiating app (%v) failed. Output:\n%v", strings.Join(cmd.Args, " "), string(output))
	instanceName := strings.TrimSpace(string(output))
	fmt.Println("Instantiated", instanceName)
	cmd = exec.Command(device, "run", instanceName)
	setCredentialsEnv(cmd)
	output, err = cmd.CombinedOutput()
	dieIfErr(err, "Starting app (%v) failed. Output:\n%v", strings.Join(cmd.Args, " "), string(output))
	return instanceName
}

func main() {
	flag.Parse()
	if len(flag.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s <app> <arguments ... >\n", os.Args[0])
		os.Exit(1)
	}
	setupWorkDir()
	cleanupOnDeath = func() {
		os.RemoveAll(workDir)
	}
	defer os.RemoveAll(workDir)
	vcloud = buildV23Binary(vcloudBin)
	device = buildV23Binary(deviceBin)
	dmBins := buildDMBinaries()
	archive := createArchive(append(dmBins, getPath(devicexRepo, devicex)))
	vmOpts := backend.VcloudVMOptions{VcloudBinary: vcloud}
	vm, vmInstanceName, vmInstanceIP := setupInstance(vmOpts)
	cleanupOnDeath = func() {
		fmt.Fprintf(os.Stderr, "Deleting VM instance ...\n")
		err := vm.Delete()
		fmt.Fprintf(os.Stderr, "Removing tmp files ...\n")
		os.RemoveAll(workDir)
		dieIfErr(err, "Deleting VM instance failed")
	}
	installArchive(archive, vmInstanceName)
	publicKey, pairingToken := installDevice(vmInstanceName)
	deviceAddr := net.JoinHostPort(vmInstanceIP, "8150")
	deviceName := "/" + deviceAddr
	claimDevice(deviceName, vmInstanceIP, publicKey, pairingToken, vmInstanceName)
	installationName := installApp(deviceName, vmInstanceIP)
	instanceName := startApp(installationName, "app")
	fmt.Println("Launched app.")
	fmt.Println("-------------")
	fmt.Println("See its status:")
	fmt.Printf("\t${JIRI_ROOT}/release/go/bin/device status %s\n", instanceName)
	fmt.Println("See the logs:")
	fmt.Printf("\t${JIRI_ROOT}/release/go/bin/debug glob %s/logs/*\n", instanceName)
	fmt.Println("Dump e.g. the INFO log:")
	fmt.Printf("\t${JIRI_ROOT}/release/go/bin/debug logs read %s/logs/app.INFO\n", instanceName)
	fmt.Println("Clean up by deleting the VM instance:")
	fmt.Printf("\t%s\n", vm.DeleteCommandForUser())
}
