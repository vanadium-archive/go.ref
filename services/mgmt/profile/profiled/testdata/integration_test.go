package integration_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/testutil/integration"
	"veyron.io/veyron/veyron/lib/testutil/security"
	_ "veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron2/naming"
)

var binPkgs = []string{
	"veyron.io/veyron/veyron/services/mgmt/profile/profiled",
	"veyron.io/veyron/veyron/tools/profile",
}

func profileCommandOutput(t *testing.T, expectError bool, command, binDir, credentials, mt, name, suffix string) string {
	var labelOut bytes.Buffer
	labelArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mt,
		command, naming.Join(name, suffix),
	}
	labelCmd := exec.Command(filepath.Join(binDir, "profile"), labelArgs...)
	labelCmd.Stdout = &labelOut
	labelCmd.Stderr = &labelOut
	err := labelCmd.Run()
	if err != nil && !expectError {
		t.Fatalf("%q failed: %v\n%v", strings.Join(labelCmd.Args, " "), err, labelOut.String())
	}
	if err == nil && expectError {
		t.Fatalf("%q did not fail when it should", strings.Join(labelCmd.Args, " "))
	}
	return strings.TrimSpace(labelOut.String())
}

func putProfile(t *testing.T, binDir, credentials, mt, name, suffix string) {
	var putOut bytes.Buffer
	putArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mt,
		"put", naming.Join(name, suffix),
	}
	putCmd := exec.Command(filepath.Join(binDir, "profile"), putArgs...)
	putCmd.Stdout = &putOut
	putCmd.Stderr = &putOut
	if err := putCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(putCmd.Args, " "), err, putOut.String())
	}
}

func removeProfile(t *testing.T, binDir, credentials, mt, name, suffix string) {
	var removeOut bytes.Buffer
	removeArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mt,
		"remove", naming.Join(name, suffix),
	}
	removeCmd := exec.Command(filepath.Join(binDir, "profile"), removeArgs...)
	removeCmd.Stdout = &removeOut
	removeCmd.Stderr = &removeOut
	if err := removeCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(removeCmd.Args, " "), err, removeOut.String())
	}
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestProfileRepository(t *testing.T) {
	// Build the required binaries.
	binDir, cleanup, err := integration.BuildPkgs(binPkgs)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer cleanup()

	// Start a root mount table.
	shell, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("NewShell() failed: %v", err)
	}
	defer shell.Cleanup(os.Stdin, os.Stderr)
	handle, mt, err := integration.StartRootMT(shell)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer handle.CloseStdin()

	// Generate credentials.
	root := security.NewPrincipal("root")
	credentials := security.NewVeyronCredentials(root, "test-credentials")
	defer os.RemoveAll(credentials)

	// Start the profile repository.
	profileRepoBin := filepath.Join(binDir, "profiled")
	profileRepoName := "test-profile-repo"
	profileRepoStore, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	defer os.RemoveAll(profileRepoStore)
	args := []string{
		"-name=" + profileRepoName, "-store=" + profileRepoStore,
		"-veyron.tcp.address=127.0.0.1:0",
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mt,
	}
	serverProcess, err := integration.StartServer(profileRepoBin, args)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer serverProcess.Kill()

	// Create a profile.
	const profile = "test-profile"
	putProfile(t, binDir, credentials, mt, profileRepoName, profile)

	// Retrieve the profile label and check it matches the
	// expected label.
	profileLabel := profileCommandOutput(t, false, "label", binDir, credentials, mt, profileRepoName, profile)
	if got, want := profileLabel, "example"; got != want {
		t.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Retrieve the profile description and check it matches the
	// expected description.
	profileDesc := profileCommandOutput(t, false, "description", binDir, credentials, mt, profileRepoName, profile)
	if got, want := profileDesc, "Example profile to test the profile manager implementation."; got != want {
		t.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Retrieve the profile specification and check it matches the
	// expected specification.
	profileSpec := profileCommandOutput(t, false, "specification", binDir, credentials, mt, profileRepoName, profile)
	if got, want := profileSpec, `profile.Specification{Arch:"amd64", Description:"Example profile to test the profile manager implementation.", Format:"ELF", Libraries:map[profile.Library]struct {}{profile.Library{Name:"foo", MajorVersion:"1", MinorVersion:"0"}:struct {}{}}, Label:"example", OS:"linux"}`; got != want {
		t.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Remove the profile.
	removeProfile(t, binDir, credentials, mt, profileRepoName, profile)

	// Check that the profile no longer exists.
	profileCommandOutput(t, true, "label", binDir, credentials, mt, profileRepoName, profile)
	profileCommandOutput(t, true, "description", binDir, credentials, mt, profileRepoName, profile)
	profileCommandOutput(t, true, "specification", binDir, credentials, mt, profileRepoName, profile)
}
