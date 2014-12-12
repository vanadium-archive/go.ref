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
	"veyron.io/veyron/veyron/services/mgmt/application/applicationd",
	"veyron.io/veyron/veyron/tools/application",
}

func helper(t *testing.T, expectError bool, binDir, credentials, mt, cmd string, args ...string) string {
	var out bytes.Buffer
	args = append([]string{"-veyron.credentials=" + credentials, "-veyron.namespace.root=" + mt, cmd}, args...)
	command := exec.Command(filepath.Join(binDir, "application"), args...)
	command.Stdout = &out
	command.Stderr = &out
	err := command.Run()
	if err != nil && !expectError {
		t.Fatalf("%q failed: %v\n%v", strings.Join(command.Args, " "), err, out.String())
	}
	if err == nil && expectError {
		t.Fatalf("%q did not fail when it should", strings.Join(command.Args, " "))
	}
	return strings.TrimSpace(out.String())

}

func matchEnvelope(t *testing.T, expectError bool, binDir, credentials, mt, name, suffix string) string {
	return helper(t, expectError, binDir, credentials, mt, "match", naming.Join(name, suffix), "test-profile")
}

func putEnvelope(t *testing.T, binDir, credentials, mt, name, suffix, envelope string) string {
	return helper(t, false, binDir, credentials, mt, "put", naming.Join(name, suffix), "test-profile", envelope)
}

func removeEnvelope(t *testing.T, binDir, credentials, mt, name, suffix string) string {
	return helper(t, false, binDir, credentials, mt, "remove", naming.Join(name, suffix), "test-profile")
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestApplicationRepository(t *testing.T) {
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
	serverCred := security.NewVeyronCredentials(root, "server")
	defer os.RemoveAll(serverCred)
	clientCred := security.NewVeyronCredentials(root, "server/client")
	defer os.RemoveAll(clientCred)

	// Start the application repository.
	appRepoBin := filepath.Join(binDir, "applicationd")
	appRepoName := "test-app-repo"
	appRepoStore, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	defer os.RemoveAll(appRepoStore)
	args := []string{
		"-name=" + appRepoName,
		"-store=" + appRepoStore,
		"-veyron.tcp.address=127.0.0.1:0",
		"-veyron.credentials=" + serverCred,
		"-veyron.namespace.root=" + mt,
	}
	serverProcess, err := integration.StartServer(appRepoBin, args)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer serverProcess.Kill()

	// Create an application envelope.
	appRepoSuffix := "test-application/v1"
	appEnvelopeFile, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatalf("TempFile() failed: %v", err)
	}
	defer appEnvelopeFile.Close()
	defer os.Remove(appEnvelopeFile.Name())
	wantEnvelope := `{
  "Title": "title",
  "Args": null,
  "Binary": "foo",
  "Env": null,
  "Packages": null
}`
	if _, err := appEnvelopeFile.Write([]byte(wantEnvelope)); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	putEnvelope(t, binDir, clientCred, mt, appRepoName, appRepoSuffix, appEnvelopeFile.Name())

	// Match the application envelope.
	gotEnvelope := matchEnvelope(t, false, binDir, clientCred, mt, appRepoName, appRepoSuffix)
	if gotEnvelope != wantEnvelope {
		t.Fatalf("unexpected output: got %v, want %v", gotEnvelope, wantEnvelope)
	}

	// Remove the application envelope.
	removeEnvelope(t, binDir, clientCred, mt, appRepoName, appRepoSuffix)

	// Check that the application envelope no longer exists.
	matchEnvelope(t, true, binDir, clientCred, mt, appRepoName, appRepoSuffix)
}
