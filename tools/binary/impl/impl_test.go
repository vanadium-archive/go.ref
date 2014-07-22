package impl_test

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

	"veyron/tools/binary/impl"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mgmt/binary"
	"veyron2/services/mgmt/repository"
	"veyron2/vlog"
)

type server struct {
	suffix string
}

func (s *server) Create(ipc.ServerContext, int32) error {
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

func (s *server) Download(_ ipc.ServerContext, _ int32, stream repository.BinaryServiceDownloadStream) error {
	vlog.Infof("Download() was called. suffix=%v", s.suffix)
	stream.Send([]byte("Hello"))
	stream.Send([]byte("World"))
	return nil
}

func (s *server) DownloadURL(ipc.ServerContext) (string, int64, error) {
	vlog.Infof("DownloadURL() was called. suffix=%v", s.suffix)
	return "", 0, nil
}

func (s *server) Stat(ipc.ServerContext) ([]binary.PartInfo, error) {
	vlog.Infof("Stat() was called. suffix=%v", s.suffix)
	h := md5.New()
	text := "HelloWorld"
	h.Write([]byte(text))
	part := binary.PartInfo{Checksum: hex.EncodeToString(h.Sum(nil)), Size: int64(len(text))}
	return []binary.PartInfo{part}, nil
}

func (s *server) Upload(_ ipc.ServerContext, _ int32, stream repository.BinaryServiceUploadStream) error {
	vlog.Infof("Upload() was called. suffix=%v", s.suffix)
	for stream.Advance() {
	}
	return nil
}

type dispatcher struct {
}

func NewDispatcher() *dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(repository.NewServerBinary(&server{suffix: suffix}))
	return invoker, nil, nil
}

func startServer(t *testing.T, r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher()
	server, err := r.NewServer()
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	endpoint, err := server.Listen("tcp", "localhost:0")
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		return nil, nil, err
	}
	if err := server.Serve("", dispatcher); err != nil {
		t.Errorf("Serve failed: %v", err)
		return nil, nil, err
	}
	return server, endpoint, nil
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestBinaryClient(t *testing.T) {
	runtime := rt.Init()
	server, endpoint, err := startServer(t, runtime)
	if err != nil {
		return
	}
	defer stopServer(t, server)
	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)

	// Test the 'delete' command.
	if err := cmd.Execute([]string{"delete", naming.JoinAddressName(endpoint.String(), "//exists")}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Binary deleted successfully", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'download' command.
	dir, err := ioutil.TempDir("", "binaryimpltest")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.Remove(dir)
	file := path.Join(dir, "testfile")
	defer os.Remove(file)
	if err := cmd.Execute([]string{"download", naming.JoinAddressName(endpoint.String(), "//exists"), file}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Binary downloaded to file "+file, strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if expected := "HelloWorld"; string(buf) != expected {
		t.Errorf("Got %q, expected %q", string(buf), expected)
	}
	stdout.Reset()

	// Test the 'upload' command.
	if err := cmd.Execute([]string{"upload", naming.JoinAddressName(endpoint.String(), "//exists"), file}); err != nil {
		t.Fatalf("%v", err)
	}
}
