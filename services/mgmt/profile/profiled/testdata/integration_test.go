package integration_test

import (
	"io/ioutil"
	"os"
	"strings"
	"syscall"
	"testing"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil/integration"
	"v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron2/naming"
)

func profileCommandOutput(t *testing.T, env integration.TestEnvironment, profileBin integration.TestBinary, expectError bool, command, credentials, name, suffix string) string {
	labelArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + env.RootMT(),
		command, naming.Join(name, suffix),
	}
	labelCmd := profileBin.Start(labelArgs...)
	out := labelCmd.Output()
	err := labelCmd.Wait(os.Stdout, os.Stderr)
	if err != nil && !expectError {
		t.Fatalf("%s %q failed: %v\n%v", profileBin.Path(), strings.Join(labelArgs, " "), err, out)
	}
	if err == nil && expectError {
		t.Fatalf("%s %q did not fail when it should", profileBin.Path(), strings.Join(labelArgs, " "))
	}
	return strings.TrimSpace(out)
}

func putProfile(t *testing.T, env integration.TestEnvironment, profileBin integration.TestBinary, credentials, name, suffix string) {
	putArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + env.RootMT(),
		"put", naming.Join(name, suffix),
	}
	profileBin.Start(putArgs...).WaitOrDie(os.Stdout, os.Stderr)
}

func removeProfile(t *testing.T, env integration.TestEnvironment, profileBin integration.TestBinary, credentials, name, suffix string) {
	removeArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + env.RootMT(),
		"remove", naming.Join(name, suffix),
	}
	profileBin.Start(removeArgs...).WaitOrDie(os.Stdout, os.Stderr)
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestProfileRepository(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	// Generate credentials.
	serverCred, serverPrin := security.NewCredentials("server")
	defer os.RemoveAll(serverCred)
	clientCred, _ := security.ForkCredentials(serverPrin, "client")
	defer os.RemoveAll(clientCred)

	// Start the profile repository.
	profileRepoName := "test-profile-repo"
	profileRepoStore, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	defer os.RemoveAll(profileRepoStore)
	args := []string{
		"-name=" + profileRepoName, "-store=" + profileRepoStore,
		"-veyron.tcp.address=127.0.0.1:0",
		"-veyron.credentials=" + serverCred,
		"-veyron.namespace.root=" + env.RootMT(),
	}
	serverBin := env.BuildGoPkg("v.io/core/veyron/services/mgmt/profile/profiled")
	serverInv := serverBin.Start(args...)
	defer serverInv.Kill(syscall.SIGTERM)

	clientBin := env.BuildGoPkg("v.io/core/veyron/tools/profile")

	// Create a profile.
	const profile = "test-profile"
	putProfile(t, env, clientBin, clientCred, profileRepoName, profile)

	// Retrieve the profile label and check it matches the
	// expected label.
	profileLabel := profileCommandOutput(t, env, clientBin, false, "label", clientCred, profileRepoName, profile)
	if got, want := profileLabel, "example"; got != want {
		t.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Retrieve the profile description and check it matches the
	// expected description.
	profileDesc := profileCommandOutput(t, env, clientBin, false, "description", clientCred, profileRepoName, profile)
	if got, want := profileDesc, "Example profile to test the profile manager implementation."; got != want {
		t.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Retrieve the profile specification and check it matches the
	// expected specification.
	profileSpec := profileCommandOutput(t, env, clientBin, false, "specification", clientCred, profileRepoName, profile)
	if got, want := profileSpec, `profile.Specification{Arch:"amd64", Description:"Example profile to test the profile manager implementation.", Format:"ELF", Libraries:map[profile.Library]struct {}{profile.Library{Name:"foo", MajorVersion:"1", MinorVersion:"0"}:struct {}{}}, Label:"example", OS:"linux"}`; got != want {
		t.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Remove the profile.
	removeProfile(t, env, clientBin, clientCred, profileRepoName, profile)

	// Check that the profile no longer exists.
	profileCommandOutput(t, env, clientBin, true, "label", clientCred, profileRepoName, profile)
	profileCommandOutput(t, env, clientBin, true, "description", clientCred, profileRepoName, profile)
	profileCommandOutput(t, env, clientBin, true, "specification", clientCred, profileRepoName, profile)
}
