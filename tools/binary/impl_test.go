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
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/binary"
	"v.io/v23/services/mgmt/repository"
	"v.io/v23/services/security/access"
	"v.io/x/lib/vlog"

	"v.io/core/veyron/lib/testutil"
	_ "v.io/core/veyron/profiles"
)

type server struct {
	suffix string
}

func (s *server) Create(ipc.ServerContext, int32, repository.MediaInfo) error {
	vlog.Infof("Create() was called. suffix=%v", s.suffix)
	return nil
}

func (s *server) Delete(ipc.ServerContext) error {
	vlog.Infof("Delete() was called. suffix=%v", s.suffix)
	if s.suffix != "exists" {
		return fmt.Errorf("binary doesn't exist: %v", s.suffix)
	}
	return nil
}

func (s *server) Download(ctx repository.BinaryDownloadContext, _ int32) error {
	vlog.Infof("Download() was called. suffix=%v", s.suffix)
	sender := ctx.SendStream()
	sender.Send([]byte("Hello"))
	sender.Send([]byte("World"))
	return nil
}

func (s *server) DownloadURL(ipc.ServerContext) (string, int64, error) {
	vlog.Infof("DownloadURL() was called. suffix=%v", s.suffix)
	if s.suffix != "" {
		return "", 0, fmt.Errorf("non-empty suffix: %v", s.suffix)
	}
	return "test-download-url", 0, nil
}

func (s *server) Stat(ipc.ServerContext) ([]binary.PartInfo, repository.MediaInfo, error) {
	vlog.Infof("Stat() was called. suffix=%v", s.suffix)
	h := md5.New()
	text := "HelloWorld"
	h.Write([]byte(text))
	part := binary.PartInfo{Checksum: hex.EncodeToString(h.Sum(nil)), Size: int64(len(text))}
	return []binary.PartInfo{part}, repository.MediaInfo{Type: "text/plain"}, nil
}

func (s *server) Upload(ctx repository.BinaryUploadContext, _ int32) error {
	vlog.Infof("Upload() was called. suffix=%v", s.suffix)
	rStream := ctx.RecvStream()
	for rStream.Advance() {
	}
	return nil
}

func (s *server) GetACL(ipc.ServerContext) (acl access.TaggedACLMap, etag string, err error) {
	return nil, "", nil
}

func (s *server) SetACL(ctx ipc.ServerContext, acl access.TaggedACLMap, etag string) error {
	return nil
}

type dispatcher struct {
}

func NewDispatcher() ipc.Dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.BinaryServer(&server{suffix: suffix}), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (ipc.Server, naming.Endpoint, error) {
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

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestBinaryClient(t *testing.T) {
	var shutdown v23.Shutdown
	gctx, shutdown = testutil.InitForTest()
	defer shutdown()

	server, endpoint, err := startServer(t, gctx)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var out bytes.Buffer
	cmd.Init(nil, &out, &out)

	// Test the 'delete' command.
	if err := cmd.Execute([]string{"delete", naming.JoinAddressName(endpoint.String(), "exists")}); err != nil {
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
	if err := cmd.Execute([]string{"download", naming.JoinAddressName(endpoint.String(), "exists"), file}); err != nil {
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
	if err := cmd.Execute([]string{"upload", naming.JoinAddressName(endpoint.String(), "exists"), file}); err != nil {
		t.Fatalf("%v failed: %v\n%v", "upload", err, out.String())
	}
	out.Reset()

	// Test the 'url' command.
	if err := cmd.Execute([]string{"url", naming.JoinAddressName(endpoint.String(), "")}); err != nil {
		t.Fatalf("%v failed: %v\n%v", "url", err, out.String())
	}
	if expected, got := "test-download-url", strings.TrimSpace(out.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
}
