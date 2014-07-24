package impl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "veyron/lib/testutil"

	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/services/mgmt/build"
)

func init() {
	rt.Init()
}

// startServer starts the build server.
func startServer(t *testing.T) (build.Build, func()) {
	root := os.Getenv("VEYRON_ROOT")
	if root == "" {
		t.Fatalf("VEYRON_ROOT is not set")
	}
	gobin := filepath.Join(root, "environment", "go", "bin", "go")
	server, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	unpublished := ""
	if err := server.Serve(unpublished, ipc.SoloDispatcher(build.NewServerBuild(NewInvoker(gobin)), nil)); err != nil {
		t.Fatalf("Serve(%q) failed: %v", unpublished, err)
	}
	name := "/" + endpoint.String()
	client, err := build.BindBuild(name)
	if err != nil {
		t.Fatalf("BindBuild(%v) failed: %v", name, err)
	}
	return client, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func invokeBuild(t *testing.T, client build.Build, files []build.File) ([]byte, []build.File, error) {
	stream, err := client.Build(rt.R().NewContext())
	if err != nil {
		t.Errorf("Build() failed: %v", err)
		return nil, nil, err
	}
	for _, file := range files {
		if err := stream.Send(file); err != nil {
			t.Logf("Send() failed: %v", err)
			stream.Cancel()
			return nil, nil, err
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Logf("CloseSend() failed: %v", err)
		stream.Cancel()
		return nil, nil, err
	}
	bins := make([]build.File, 0)
	for stream.Advance() {
		bin := stream.Value()
		bins = append(bins, bin)
	}

	if err := stream.Err(); err != nil {
		t.Logf("Recv() failed: %v", err)
		return nil, nil, err
	}

	output, err := stream.Finish()
	if err != nil {
		t.Logf("Finish() failed: %v", err)
		stream.Cancel()
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

// TestFailure checks that the build server fails to build a package
// consisting of an empty file.
func TestFailure(t *testing.T) {
	client, cleanup := startServer(t)
	defer cleanup()

	files := []build.File{
		build.File{
			Name:     "test/main.go",
			Contents: []byte(""),
		},
	}
	if _, _, err := invokeBuild(t, client, files); err == nil {
		t.FailNow()
	}
}
