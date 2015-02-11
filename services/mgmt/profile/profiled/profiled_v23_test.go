package main_test

import (
	"os"
	"strings"

	"v.io/core/veyron/lib/testutil/v23tests"
	"v.io/core/veyron2/naming"
)

//go:generate v23 test generate

func profileCommandOutput(i v23tests.T, profileBin v23tests.TestBinary, expectError bool, command, name, suffix string) string {
	labelArgs := []string{
		command, naming.Join(name, suffix),
	}
	labelCmd := profileBin.Start(labelArgs...)
	out := labelCmd.Output()
	err := labelCmd.Wait(os.Stdout, os.Stderr)
	if err != nil && !expectError {
		i.Fatalf("%s %q failed: %v\n%v", profileBin.Path(), strings.Join(labelArgs, " "), err, out)
	}
	if err == nil && expectError {
		i.Fatalf("%s %q did not fail when it should", profileBin.Path(), strings.Join(labelArgs, " "))
	}
	return strings.TrimSpace(out)
}

func putProfile(i v23tests.T, profileBin v23tests.TestBinary, name, suffix string) {
	putArgs := []string{
		"put", naming.Join(name, suffix),
	}
	profileBin.Start(putArgs...).WaitOrDie(os.Stdout, os.Stderr)
}

func removeProfile(i v23tests.T, profileBin v23tests.TestBinary, name, suffix string) {
	removeArgs := []string{
		"remove", naming.Join(name, suffix),
	}
	profileBin.Start(removeArgs...).WaitOrDie(os.Stdout, os.Stderr)
}

func V23TestProfileRepository(i v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	// Start the profile repository.
	profileRepoName := "test-profile-repo"
	profileRepoStore := i.TempDir()
	args := []string{
		"-name=" + profileRepoName, "-store=" + profileRepoStore,
		"-veyron.tcp.address=127.0.0.1:0",
	}
	i.BuildGoPkg("v.io/core/veyron/services/mgmt/profile/profiled").Start(args...)

	clientBin := i.BuildGoPkg("v.io/core/veyron/tools/profile")

	// Create a profile.
	const profile = "test-profile"
	putProfile(i, clientBin, profileRepoName, profile)

	// Retrieve the profile label and check it matches the
	// expected label.
	profileLabel := profileCommandOutput(i, clientBin, false, "label", profileRepoName, profile)
	if got, want := profileLabel, "example"; got != want {
		i.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Retrieve the profile description and check it matches the
	// expected description.
	profileDesc := profileCommandOutput(i, clientBin, false, "description", profileRepoName, profile)
	if got, want := profileDesc, "Example profile to test the profile manager implementation."; got != want {
		i.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Retrieve the profile specification and check it matches the
	// expected specification.
	profileSpec := profileCommandOutput(i, clientBin, false, "specification", profileRepoName, profile)
	if got, want := profileSpec, `profile.Specification{Label:"example", Description:"Example profile to test the profile manager implementation.", Arch:"amd64", OS:"linux", Format:"ELF", Libraries:map[profile.Library]struct {}{profile.Library{Name:"foo", MajorVersion:"1", MinorVersion:"0"}:struct {}{}}}`; got != want {
		i.Fatalf("unexpected output: got %v, want %v", got, want)
	}

	// Remove the profile.
	removeProfile(i, clientBin, profileRepoName, profile)

	// Check that the profile no longer exists.
	profileCommandOutput(i, clientBin, true, "label", profileRepoName, profile)
	profileCommandOutput(i, clientBin, true, "description", profileRepoName, profile)
	profileCommandOutput(i, clientBin, true, "specification", profileRepoName, profile)
}
