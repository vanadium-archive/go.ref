package main_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"v.io/x/ref/lib/testutil/security"
	"v.io/x/ref/lib/testutil/v23tests"
)

//go:generate v23 test generate

var testProgram = `package main

import "fmt"

func main() { fmt.Println("Hello World!") }
`

func V23TestBuildServerIntegration(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	// Generate credentials.
	serverCred, serverPrin := security.NewCredentials("server")
	defer os.RemoveAll(serverCred)
	clientCred, _ := security.ForkCredentials(serverPrin, "client")
	defer os.RemoveAll(clientCred)

	// Start the build server.
	buildServerBin := i.BuildGoPkg("v.io/x/ref/services/mgmt/build/buildd")
	buildServerName := "test-build-server"
	goBin, err := exec.LookPath("go")
	if err != nil {
		i.Fatalf("%v", err)
	}
	goRoot := runtime.GOROOT()
	args := []string{
		"-name=" + buildServerName, "-gobin=" + goBin, "-goroot=" + goRoot,
		"-veyron.tcp.address=127.0.0.1:0",
		"-veyron.credentials=" + serverCred,
	}
	buildServerBin.Start(args...)

	// Create and build a test source file.
	testGoPath := i.NewTempDir()
	testBinDir := filepath.Join(testGoPath, "bin")
	if err := os.MkdirAll(testBinDir, os.FileMode(0700)); err != nil {
		i.Fatalf("MkdirAll(%v) failed: %v", testBinDir, err)
	}
	testBinFile := filepath.Join(testBinDir, "test")
	testSrcDir := filepath.Join(testGoPath, "src", "test")
	if err := os.MkdirAll(testSrcDir, os.FileMode(0700)); err != nil {
		i.Fatalf("MkdirAll(%v) failed: %v", testSrcDir, err)
	}
	testSrcFile := filepath.Join(testSrcDir, "test.go")
	if err := ioutil.WriteFile(testSrcFile, []byte(testProgram), os.FileMode(0600)); err != nil {
		i.Fatalf("WriteFile(%v) failed: %v", testSrcFile, err)
	}
	buildArgs := []string{
		"-veyron.credentials=" + clientCred,
		"build", buildServerName, "test",
	}
	buildEnv := []string{"GOPATH=" + testGoPath, "GOROOT=" + goRoot, "TMPDIR=" + testBinDir}
	buildBin := i.BuildGoPkg("v.io/x/ref/cmd/build")
	buildBin.WithEnv(buildEnv...).Start(buildArgs...).WaitOrDie(os.Stdout, os.Stderr)
	var testOut bytes.Buffer
	testCmd := exec.Command(testBinFile)
	testCmd.Stdout = &testOut
	testCmd.Stderr = &testOut
	if err := testCmd.Run(); err != nil {
		i.Fatalf("%q failed: %v\n%v", strings.Join(testCmd.Args, " "), err, testOut.String())
	}
	if got, want := strings.TrimSpace(testOut.String()), "Hello World!"; got != want {
		i.Fatalf("unexpected output: got %v, want %v", got, want)
	}
}
