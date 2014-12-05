package integration_test

import (
	"bytes"
	"fmt"
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
)

var binPkgs = []string{
	"veyron.io/veyron/veyron/services/mgmt/build/buildd",
	"veyron.io/veyron/veyron/tools/build",
}

var testProgram = `package main

import "fmt"

func main() { fmt.Println("Hello World!") }
`

func goRoot(bin string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(bin, "env", "GOROOT")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%q failed: %v\n%v", strings.Join(cmd.Args, " "), err, out.String())
	}
	cleanOut := strings.TrimSpace(out.String())
	if cleanOut == "" {
		return "", fmt.Errorf("%v does not set GOROOT", bin)
	}
	return cleanOut, nil
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestBuild(t *testing.T) {
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
	handle, mtName, err := integration.StartRootMT(shell)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer handle.CloseStdin()

	// Generate credentials.
	root := security.NewPrincipal("root")
	credentials := security.NewVeyronCredentials(root, "test-credentials")
	defer os.RemoveAll(credentials)

	// Start the build server.
	buildServerBin := filepath.Join(binDir, "buildd")
	buildServerName := "test-build-server"
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("%v", err)
	}
	goRoot, err := goRoot(goBin)
	if err != nil {
		t.Fatalf("%v", err)
	}
	args := []string{
		"-name=" + buildServerName, "-gobin=" + goBin, "-goroot=" + goRoot,
		"-veyron.tcp.address=127.0.0.1:0",
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mtName,
	}
	serverProcess, err := integration.StartServer(buildServerBin, args)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer serverProcess.Kill()

	// Create and build a test source file.
	testGoPath, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	defer os.RemoveAll(testGoPath)
	testBinDir := filepath.Join(testGoPath, "bin")
	if err := os.MkdirAll(testBinDir, os.FileMode(0700)); err != nil {
		t.Fatalf("MkdirAll(%v) failed: %v", testBinDir, err)
	}
	testBinFile := filepath.Join(testBinDir, "test")
	testSrcDir := filepath.Join(testGoPath, "src", "test")
	if err := os.MkdirAll(testSrcDir, os.FileMode(0700)); err != nil {
		t.Fatalf("MkdirAll(%v) failed: %v", testSrcDir, err)
	}
	testSrcFile := filepath.Join(testSrcDir, "test.go")
	if err := ioutil.WriteFile(testSrcFile, []byte(testProgram), os.FileMode(0600)); err != nil {
		t.Fatalf("WriteFile(%v) failed: %v", testSrcFile, err)
	}
	var buildOut bytes.Buffer
	buildArgs := []string{
		"-veyron.credentials=" + credentials,
		"-veyron.namespace.root=" + mtName,
		"build", buildServerName, "test",
	}
	buildCmd := exec.Command(filepath.Join(binDir, "build"), buildArgs...)
	buildCmd.Stdout = &buildOut
	buildCmd.Stderr = &buildOut
	buildCmd.Env = append(buildCmd.Env, "GOPATH="+testGoPath, "GOROOT="+goRoot, "TMPDIR="+testBinDir)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(buildCmd.Args, " "), err, buildOut.String())
	}
	var testOut bytes.Buffer
	testCmd := exec.Command(testBinFile)
	testCmd.Stdout = &testOut
	testCmd.Stderr = &testOut
	if err := testCmd.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%v", strings.Join(testCmd.Args, " "), err, testOut.String())
	}
	if got, want := strings.TrimSpace(testOut.String()), "Hello World!"; got != want {
		t.Fatalf("unexpected output: got %v, want %v", got, want)
	}
}
