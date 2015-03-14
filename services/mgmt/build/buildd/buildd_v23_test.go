package main_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate

var testProgram = `package main

import "fmt"

func main() { fmt.Println("Hello World!") }
`

func V23TestBuildServerIntegration(i *v23tests.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		i.Fatalf("%v", err)
	}
	goRoot := runtime.GOROOT()

	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	// Build binaries for the client and server.
	// Since ACLs are not setup on the server, the client must pass the
	// default authorization policy, i.e., must be a "delegate" of the
	// server.
	var (
		buildServerBin = binaryWithCredentials(i, "buildd", "v.io/x/ref/services/mgmt/build/buildd")
		buildBin       = binaryWithCredentials(i, "buildd/client", "v.io/x/ref/cmd/build")
	)

	// Start the build server.
	buildServerName := "test-build-server"
	buildServerBin.Start(
		"-name="+buildServerName,
		"-gobin="+goBin,
		"-goroot="+goRoot,
		"-veyron.tcp.address=127.0.0.1:0")

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
	buildBin.WithEnv(
		"GOPATH="+testGoPath,
		"GOROOT="+goRoot,
		"TMPDIR="+testBinDir).Start(
		"build",
		buildServerName,
		"test").WaitOrDie(os.Stdout, os.Stderr)
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

func binaryWithCredentials(i *v23tests.T, extension, pkgpath string) *v23tests.Binary {
	creds, err := i.Shell().NewChildCredentials(extension)
	if err != nil {
		i.Fatalf("NewCustomCredentials (for %q) failed: %v", pkgpath, err)
	}
	b := i.BuildV23Pkg(pkgpath)
	return b.WithStartOpts(b.StartOpts().WithCustomCredentials(creds))
}
