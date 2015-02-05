package integration_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil/integration"
	"v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
)

var testProgram = `package main

import "fmt"

func main() { fmt.Println("Hello World!") }
`

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestBuildServerIntegration(t *testing.T) {
	env := integration.New(t)
	defer env.Cleanup()

	// Generate credentials.
	serverCred, serverPrin := security.NewCredentials("server")
	defer os.RemoveAll(serverCred)
	clientCred, _ := security.ForkCredentials(serverPrin, "client")
	defer os.RemoveAll(clientCred)

	// Start the build server.
	buildServerBin := env.BuildGoPkg("v.io/core/veyron/services/mgmt/build/buildd")
	buildServerName := "test-build-server"
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("%v", err)
	}
	goRoot := runtime.GOROOT()
	args := []string{
		"-name=" + buildServerName, "-gobin=" + goBin, "-goroot=" + goRoot,
		"-veyron.tcp.address=127.0.0.1:0",
		"-veyron.credentials=" + serverCred,
		"-veyron.namespace.root=" + env.RootMT(),
	}
	serverProcess := buildServerBin.Start(args...)
	defer serverProcess.Kill(syscall.SIGTERM)

	// Create and build a test source file.
	testGoPath := env.TempDir()
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
	buildArgs := []string{
		"-veyron.credentials=" + clientCred,
		"-veyron.namespace.root=" + env.RootMT(),
		"build", buildServerName, "test",
	}
	buildEnv := []string{"GOPATH=" + testGoPath, "GOROOT=" + goRoot, "TMPDIR=" + testBinDir}
	buildBin := env.BuildGoPkg("v.io/core/veyron/tools/build")
	buildBin.WithEnv(buildEnv).Start(buildArgs...).WaitOrDie(os.Stdout, os.Stderr)
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
