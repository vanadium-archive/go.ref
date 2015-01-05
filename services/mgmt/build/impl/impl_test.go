package impl

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/services/mgmt/build"

	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/profiles"
)

var globalRT veyron2.Runtime

func init() {
	testutil.Init()
	var err error
	if globalRT, err = rt.New(); err != nil {
		panic(err)
	}
}

// findGoBinary returns the path to the given Go binary and
// the GOROOT environment variable to use.
func findGoBinary(t *testing.T, name string) (bin, goroot string) {
	root := os.Getenv("VANADIUM_ROOT")
	if root == "" {
		t.Fatalf("VANADIUM_ROOT is not set")
	}
	envroot := filepath.Join(root, "environment", "go", runtime.GOOS, runtime.GOARCH, "go")
	envbin := filepath.Join(envroot, "bin", name)
	if _, err := os.Stat(envbin); err == nil {
		return envbin, envroot
	} else if !os.IsNotExist(err) {
		t.Fatalf("Stat(%v) failed: %v", envbin, err)
	}
	pathbin, err := exec.LookPath(name)
	if err != nil {
		if err == exec.ErrNotFound {
			t.Fatalf("%q does not exist and %q not found in PATH", envbin, name)
		} else {
			t.Fatalf("LookPath(%q) failed: %v", name, err)
		}
	}
	return pathbin, os.Getenv("GOROOT")
}

// startServer starts the build server.
func startServer(t *testing.T) (build.BuilderClientMethods, func()) {
	gobin, goroot := findGoBinary(t, "go")
	server, err := globalRT.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	endpoints, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
	unpublished := ""
	if err := server.Serve(unpublished, build.BuilderServer(NewBuilderService(gobin, goroot)), nil); err != nil {
		t.Fatalf("Serve(%q) failed: %v", unpublished, err)
	}
	name := "/" + endpoints[0].String()
	return build.BuilderClient(name), func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func invokeBuild(t *testing.T, client build.BuilderClientMethods, files []build.File) ([]byte, []build.File, error) {
	arch, opsys := getArch(), getOS()
	ctx, cancel := context.WithCancel(globalRT.NewContext())
	defer cancel()
	stream, err := client.Build(ctx, arch, opsys)
	if err != nil {
		t.Errorf("Build(%v, %v) failed: %v", err, arch, opsys)
		return nil, nil, err
	}
	sender := stream.SendStream()
	for _, file := range files {
		if err := sender.Send(file); err != nil {
			t.Logf("Send() failed: %v", err)
			return nil, nil, err
		}
	}
	if err := sender.Close(); err != nil {
		t.Logf("Close() failed: %v", err)
		return nil, nil, err
	}
	bins := make([]build.File, 0)
	rStream := stream.RecvStream()
	for rStream.Advance() {
		bins = append(bins, rStream.Value())
	}
	if err := rStream.Err(); err != nil {
		t.Logf("Advance() failed: %v", err)
		return nil, nil, err
	}
	output, err := stream.Finish()
	if err != nil {
		t.Logf("Finish() failed: %v", err)
		return nil, nil, err
	}
	return output, bins, nil
}

const mainSrc = `package main

import "fmt"

func main() {
	fmt.Println("Hello World!")
}
`

// TestSuccess checks that the build server successfully builds a
// package that depends on the standard Go library.
func TestSuccess(t *testing.T) {
	client, cleanup := startServer(t)
	defer cleanup()

	files := []build.File{
		build.File{
			Name:     "test/main.go",
			Contents: []byte(mainSrc),
		},
	}
	output, bins, err := invokeBuild(t, client, files)
	if err != nil {
		t.FailNow()
	}
	if got, expected := strings.TrimSpace(string(output)), "test"; got != expected {
		t.Fatalf("Unexpected output: got %v, expected %v", got, expected)
	}
	if got, expected := len(bins), 1; got != expected {
		t.Fatalf("Unexpected number of binaries: got %v, expected %v", got, expected)
	}
}

const fooSrc = `package foo

import "fmt"

func foo() {
	fmt.Println("Hello World!")
}
`

// TestEmpty checks that the build server successfully builds a
// package that does not produce a binary.
func TestEmpty(t *testing.T) {
	client, cleanup := startServer(t)
	defer cleanup()

	files := []build.File{
		build.File{
			Name:     "test/foo.go",
			Contents: []byte(fooSrc),
		},
	}
	output, bins, err := invokeBuild(t, client, files)
	if err != nil {
		t.FailNow()
	}
	if got, expected := strings.TrimSpace(string(output)), "test"; got != expected {
		t.Fatalf("Unexpected output: got %v, expected %v", got, expected)
	}
	if got, expected := len(bins), 0; got != expected {
		t.Fatalf("Unexpected number of binaries: got %v, expected %v", got, expected)
	}
}

const failSrc = `package main

import "fmt"

func main() {
        ...
}
`

// TestFailure checks that the build server fails to build a package
// consisting of an empty file.
func TestFailure(t *testing.T) {
	client, cleanup := startServer(t)
	defer cleanup()

	files := []build.File{
		build.File{
			Name:     "test/main.go",
			Contents: []byte(failSrc),
		},
	}
	if output, _, err := invokeBuild(t, client, files); err == nil {
		t.Logf("%v", string(output))
		t.FailNow()
	}
}
