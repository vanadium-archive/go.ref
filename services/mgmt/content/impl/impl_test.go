package impl

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"io/ioutil"
	"os"
	"testing"

	"veyron2"
	"veyron2/rt"
	"veyron2/services/mgmt/content"
)

const (
	veyronPrefix = "veyron_content_server"
)

// quote is a content example used in this test.
var (
	quote = []byte("Everything should be made as simple as possible, but not simpler.")
)

// invokeUpload invokes the Upload RPC using the given client stub
// <stub> and streams the given content <content> to it.
func invokeUpload(t *testing.T, stub content.Content, content []byte) (string, error) {
	stream, err := stub.Upload()
	if err != nil {
		return "", err
	}
	if err := stream.Send(content); err != nil {
		stream.Finish()
		return "", err
	}
	if err := stream.CloseSend(); err != nil {
		stream.Finish()
		return "", err
	}
	name, err := stream.Finish()
	if err != nil {
		return "", err
	}
	return name, nil
}

// invokeDownload invokes the Download RPC using the given client stub
// <stub> and streams content from to it.
func invokeDownload(t *testing.T, stub content.Content) ([]byte, error) {
	stream, err := stub.Download()
	if err != nil {
		return nil, err
	}
	defer stream.Finish()
	output, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	return output, nil
}

// invokeDelete invokes the Delete RPC using the given client stub
// <stub>.
func invokeDelete(t *testing.T, stub content.Content) error {
	return stub.Delete()
}

// testInterface tests the content manager interface using the given
// depth for hierarchy of content objects.
func testInterface(t *testing.T, runtime veyron2.Runtime, depth int) {
	root, err := ioutil.TempDir("", veyronPrefix)
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}

	// Setup and start the content manager server.
	server, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	dispatcher := NewDispatcher(root, depth, nil)
	suffix := ""
	if err := server.Register(suffix, dispatcher); err != nil {
		t.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	name := ""
	if err := server.Publish(name); err != nil {
		t.Fatalf("Publish(%v) failed: %v", name, err)
	}

	// Create client stubs for talking to the server.
	stub, err := content.BindContent("/" + endpoint.String())
	if err != nil {
		t.Fatalf("BindContent() failed: %v", err)
	}

	// Upload
	contentName, err := invokeUpload(t, stub, quote)
	if err != nil {
		t.Fatalf("invokeUpload() failed: %v", err)
	}
	h := md5.New()
	h.Write(quote)
	checksum := hex.EncodeToString(h.Sum(nil))
	if contentName != checksum {
		t.Fatalf("Unexpected checksum: expected %v, got %v", checksum, contentName)
	}

	// Download
	stub, err = content.BindContent("/" + endpoint.String() + "/" + checksum)
	if err != nil {
		t.Fatalf("BindContent() failed: %v", err)
	}
	output, err := invokeDownload(t, stub)
	if err != nil {
		t.Fatalf("invokedDownload() failed: %v", err)
	}
	if bytes.Compare(output, quote) != 0 {
		t.Fatalf("Unexpected output: expected %v, got %v", quote, output)
	}

	// Delete
	if err := invokeDelete(t, stub); err != nil {
		t.Fatalf("invokedDelete() failed: %v", err)
	}

	// Check that any directories and files that were created to
	// represent the content objects have been garbage collected.
	if err := os.Remove(root); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Shutdown the content manager server.
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

// TestHierarchy checks that the content manager works correctly for
// all possible valid values of the depth used for the directory
// hierarchy that stores content objects in the local file system.
func TestHierarchy(t *testing.T) {
	runtime := rt.Init()
	defer runtime.Shutdown()
	for i := 0; i < md5.Size; i++ {
		testInterface(t, runtime, i)
	}
}
