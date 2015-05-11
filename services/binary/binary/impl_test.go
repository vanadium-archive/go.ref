// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/binary"
	"v.io/v23/services/repository"
	"v.io/x/lib/cmdline"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

//go:generate v23 test generate

type server struct {
	suffix string
}

func (s *server) Create(*context.T, rpc.ServerCall, int32, repository.MediaInfo) error {
	vlog.Infof("Create() was called. suffix=%v", s.suffix)
	return nil
}

func (s *server) Delete(*context.T, rpc.ServerCall) error {
	vlog.Infof("Delete() was called. suffix=%v", s.suffix)
	if s.suffix != "exists" {
		return fmt.Errorf("binary doesn't exist: %v", s.suffix)
	}
	return nil
}

func (s *server) Download(_ *context.T, call repository.BinaryDownloadServerCall, _ int32) error {
	vlog.Infof("Download() was called. suffix=%v", s.suffix)
	sender := call.SendStream()
	sender.Send([]byte("Hello"))
	sender.Send([]byte("World"))
	return nil
}

func (s *server) DownloadUrl(*context.T, rpc.ServerCall) (string, int64, error) {
	vlog.Infof("DownloadUrl() was called. suffix=%v", s.suffix)
	if s.suffix != "" {
		return "", 0, fmt.Errorf("non-empty suffix: %v", s.suffix)
	}
	return "test-download-url", 0, nil
}

func (s *server) Stat(*context.T, rpc.ServerCall) ([]binary.PartInfo, repository.MediaInfo, error) {
	vlog.Infof("Stat() was called. suffix=%v", s.suffix)
	h := md5.New()
	text := "HelloWorld"
	h.Write([]byte(text))
	part := binary.PartInfo{Checksum: hex.EncodeToString(h.Sum(nil)), Size: int64(len(text))}
	return []binary.PartInfo{part}, repository.MediaInfo{Type: "text/plain"}, nil
}

func (s *server) Upload(_ *context.T, call repository.BinaryUploadServerCall, _ int32) error {
	vlog.Infof("Upload() was called. suffix=%v", s.suffix)
	rStream := call.RecvStream()
	for rStream.Advance() {
	}
	return nil
}

func (s *server) GetPermissions(*context.T, rpc.ServerCall) (perms access.Permissions, version string, err error) {
	return nil, "", nil
}

func (s *server) SetPermissions(_ *context.T, _ rpc.ServerCall, perms access.Permissions, version string) error {
	return nil
}

type dispatcher struct {
}

func NewDispatcher() rpc.Dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.BinaryServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (rpc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher()
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	endpoints, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		return nil, nil, err
	}
	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Errorf("ServeDispatcher failed: %v", err)
		return nil, nil, err
	}
	return server, endpoints[0], nil
}

func stopServer(t *testing.T, server rpc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestBinaryClient(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	server, endpoint, err := startServer(t, ctx)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	var out bytes.Buffer
	env := &cmdline.Env{Stdout: &out, Stderr: &out}

	// Test the 'delete' command.
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"delete", naming.JoinAddressName(endpoint.String(), "exists")}); err != nil {
		t.Fatalf("%v failed: %v\n%v", "delete", err, out.String())
	}
	if expected, got := "Binary deleted successfully", strings.TrimSpace(out.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	out.Reset()

	// Test the 'download' command.
	dir, err := ioutil.TempDir("", "binaryimpltest")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(dir)
	file := path.Join(dir, "testfile")
	defer os.Remove(file)
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"download", naming.JoinAddressName(endpoint.String(), "exists"), file}); err != nil {
		t.Fatalf("%v failed: %v\n%v", "download", err, out.String())
	}
	if expected, got := "Binary downloaded to file "+file, strings.TrimSpace(out.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if expected := "HelloWorld"; string(buf) != expected {
		t.Errorf("Got %q, expected %q", string(buf), expected)
	}
	out.Reset()

	// Test the 'upload' command.
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"upload", naming.JoinAddressName(endpoint.String(), "exists"), file}); err != nil {
		t.Fatalf("%v failed: %v\n%v", "upload", err, out.String())
	}
	out.Reset()

	// Test the 'url' command.
	if err := v23cmd.ParseAndRun(cmdRoot, ctx, env, []string{"url", naming.JoinAddressName(endpoint.String(), "")}); err != nil {
		t.Fatalf("%v failed: %v\n%v", "url", err, out.String())
	}
	if expected, got := "test-download-url", strings.TrimSpace(out.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
}
