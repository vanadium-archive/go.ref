package impl_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"veyron/tools/content/impl"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mgmt/content"
	"veyron2/vlog"
)

type server struct {
	suffix string
}

func (s *server) Delete(ipc.Context) error {
	vlog.VI(2).Infof("Delete() was called. suffix=%v", s.suffix)
	if s.suffix != "exists" {
		return fmt.Errorf("content doesn't exist: %v", s.suffix)
	}
	return nil
}

func (s *server) Download(_ ipc.Context, stream content.ContentServiceDownloadStream) error {
	vlog.VI(2).Infof("Download() was called. suffix=%v", s.suffix)
	stream.Send([]byte("Hello"))
	stream.Send([]byte("World"))
	return nil
}

func (s *server) Upload(_ ipc.Context, stream content.ContentServiceUploadStream) (string, error) {
	vlog.VI(2).Infof("Upload() was called. suffix=%v", s.suffix)
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
	return "newcontentid", nil
}

type dispatcher struct {
}

func NewDispatcher() *dispatcher {
	return &dispatcher{}
}

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(content.NewServerContent(&server{suffix: suffix}))
	return invoker, nil, nil
}

func startServer(t *testing.T, r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher()
	server, err := r.NewServer()
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	if err := server.Register("", dispatcher); err != nil {
		t.Errorf("Register failed: %v", err)
		return nil, nil, err
	}
	endpoint, err := server.Listen("tcp", "localhost:0")
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		return nil, nil, err
	}
	return server, endpoint, nil
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestContentClient(t *testing.T) {
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
	if expected, got := "Content deleted successfully", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
	stdout.Reset()

	// Test the 'download' command.
	dir, err := ioutil.TempDir("", "contentimpltest")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.Remove(dir)
	file := path.Join(dir, "testfile")
	defer os.Remove(file)
	if err := cmd.Execute([]string{"download", naming.JoinAddressName(endpoint.String(), "//exists"), file}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "Content downloaded to file "+file, strings.TrimSpace(stdout.String()); got != expected {
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
	if err := cmd.Execute([]string{"upload", naming.JoinAddressName(endpoint.String(), ""), file}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "newcontentid", strings.TrimSpace(stdout.String()); got != expected {
		t.Errorf("Got %q, expected %q", got, expected)
	}
}
