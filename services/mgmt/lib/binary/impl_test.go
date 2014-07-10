package binary

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"veyron/lib/testutil"
	"veyron/services/mgmt/binary/impl"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

const (
	veyronPrefix = "veyron_binary_repository"
)

func init() {
	rt.Init()
}

func setupRepository(t *testing.T) (string, func()) {
	// Setup the root of the binary repository.
	root, err := ioutil.TempDir("", veyronPrefix)
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	path, perm := filepath.Join(root, impl.VersionFile), os.FileMode(0600)
	if err := ioutil.WriteFile(path, []byte(impl.Version), perm); err != nil {
		vlog.Fatalf("WriteFile(%v, %v, %v) failed: %v", path, impl.Version, perm, err)
	}
	// Setup and start the binary repository server.
	server, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	depth := 2
	dispatcher, err := impl.NewDispatcher(root, depth, nil)
	if err != nil {
		t.Fatalf("NewDispatcher(%v, %v, %v) failed: %v", root, depth, nil, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	suffix := ""
	if err := server.Serve(suffix, dispatcher); err != nil {
		t.Fatalf("Serve(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	von := naming.JoinAddressName(endpoint.String(), "//test")
	return von, func() {
		if err := os.Remove(path); err != nil {
			t.Fatalf("Remove(%v) failed: %v", path, err)
		}
		// Check that any directories and files that were created to
		// represent the binary objects have been garbage collected.
		if err := os.Remove(root); err != nil {
			t.Fatalf("Remove(%v) failed: %v", root, err)
		}
		// Shutdown the binary repository server.
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

// TestBufferAPI tests the binary repository client-side library
// interface using buffers.
func TestBufferAPI(t *testing.T) {
	von, cleanup := setupRepository(t)
	defer cleanup()
	data := testutil.RandomBytes(testutil.Rand.Intn(10 << 20))
	if err := Upload(von, data); err != nil {
		t.Fatalf("Upload(%v) failed: %v", von, err)
	}
	output, err := Download(von)
	if err != nil {
		t.Fatalf("Download(%v) failed: %v", von, err)
	}
	if bytes.Compare(data, output) != 0 {
		t.Fatalf("Data mismatch:\nexpected %v %v\ngot %v %v", len(data), data[:100], len(output), output[:100])
	}
	if err := Delete(von); err != nil {
		t.Fatalf("Delete(%v) failed: %v", von, err)
	}
	if _, err := Download(von); err == nil {
		t.Fatalf("Download(%v) did not fail", von)
	}
}

// TestFileAPI tests the binary repository client-side library
// interface using files.
func TestFileAPI(t *testing.T) {
	von, cleanup := setupRepository(t)
	defer cleanup()
	// Create up to 10MB of random bytes.
	data := testutil.RandomBytes(testutil.Rand.Intn(10 << 20))
	dir, prefix := "", ""
	src, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		t.Fatalf("TempFile(%v, %v) failed: %v", dir, prefix, err)
	}
	defer os.Remove(src.Name())
	defer src.Close()
	dst, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		t.Fatalf("TempFile(%v, %v) failed: %v", dir, prefix, err)
	}
	defer os.Remove(dst.Name())
	defer dst.Close()
	if _, err := src.Write(data); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if err := UploadFromFile(von, src.Name()); err != nil {
		t.Fatalf("UploadFromFile(%v, %v) failed: %v", von, src.Name(), err)
	}
	if err := DownloadToFile(von, dst.Name()); err != nil {
		t.Fatalf("DownloadToFile(%v, %v) failed: %v", von, dst.Name(), err)
	}
	output, err := ioutil.ReadFile(dst.Name())
	if err != nil {
		t.Fatalf("ReadFile(%v) failed: %v", dst.Name(), err)
	}
	if bytes.Compare(data, output) != 0 {
		t.Fatalf("Data mismatch:\nexpected %v %v\ngot %v %v", len(data), data[:100], len(output), output[:100])
	}
	if err := Delete(von); err != nil {
		t.Fatalf("Delete(%v) failed: %v", von, err)
	}
}
