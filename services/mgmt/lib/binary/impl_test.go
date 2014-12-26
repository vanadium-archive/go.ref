package binary

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/services/mgmt/repository"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/profiles"
	"v.io/core/veyron/services/mgmt/binary/impl"
)

const (
	veyronPrefix = "veyron_binary_repository"
)

var runtime veyron2.Runtime

func init() {
	testutil.Init()

	var err error
	runtime, err = rt.New()
	if err != nil {
		panic(err)
	}
}

func setupRepository(t *testing.T) (string, func()) {
	// Setup the root of the binary repository.
	rootDir, err := ioutil.TempDir("", veyronPrefix)
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	path, perm := filepath.Join(rootDir, impl.VersionFile), os.FileMode(0600)
	if err := ioutil.WriteFile(path, []byte(impl.Version), perm); err != nil {
		vlog.Fatalf("WriteFile(%v, %v, %v) failed: %v", path, impl.Version, perm, err)
	}
	// Setup and start the binary repository server.
	server, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	depth := 2
	state, err := impl.NewState(rootDir, "http://test-root-url", depth)
	if err != nil {
		t.Fatalf("NewState(%v, %v) failed: %v", rootDir, depth, err)
	}
	dispatcher := impl.NewDispatcher(state, nil)
	endpoints, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen(%s) failed: %v", profiles.LocalListenSpec, err)
	}
	suffix := ""
	if err := server.ServeDispatcher(suffix, dispatcher); err != nil {
		t.Fatalf("Serve(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	von := naming.JoinAddressName(endpoints[0].String(), "test")
	return von, func() {
		if err := os.Remove(path); err != nil {
			t.Fatalf("Remove(%v) failed: %v", path, err)
		}
		// Check that any directories and files that were created to
		// represent the binary objects have been garbage collected.
		if err := os.RemoveAll(rootDir); err != nil {
			t.Fatalf("Remove(%v) failed: %v", rootDir, err)
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
	mediaInfo := repository.MediaInfo{Type: "application/octet-stream"}
	if err := Upload(runtime.NewContext(), von, data, mediaInfo); err != nil {
		t.Fatalf("Upload(%v) failed: %v", von, err)
	}
	output, outInfo, err := Download(runtime.NewContext(), von)
	if err != nil {
		t.Fatalf("Download(%v) failed: %v", von, err)
	}
	if bytes.Compare(data, output) != 0 {
		t.Errorf("Data mismatch:\nexpected %v %v\ngot %v %v", len(data), data[:100], len(output), output[:100])
	}
	if err := Delete(runtime.NewContext(), von); err != nil {
		t.Errorf("Delete(%v) failed: %v", von, err)
	}
	if _, _, err := Download(runtime.NewContext(), von); err == nil {
		t.Errorf("Download(%v) did not fail", von)
	}
	if !reflect.DeepEqual(mediaInfo, outInfo) {
		t.Errorf("unexpected media info: expected %v, got %v", mediaInfo, outInfo)
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
	dstdir, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("TempDir(%v, %v) failed: %v", dir, prefix, err)
	}
	defer os.RemoveAll(dstdir)
	dst, err := ioutil.TempFile(dstdir, prefix)
	if err != nil {
		t.Fatalf("TempFile(%v, %v) failed: %v", dstdir, prefix, err)
	}
	defer dst.Close()
	if _, err := src.Write(data); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if err := UploadFromFile(runtime.NewContext(), von, src.Name()); err != nil {
		t.Fatalf("UploadFromFile(%v, %v) failed: %v", von, src.Name(), err)
	}
	if err := DownloadToFile(runtime.NewContext(), von, dst.Name()); err != nil {
		t.Fatalf("DownloadToFile(%v, %v) failed: %v", von, dst.Name(), err)
	}
	output, err := ioutil.ReadFile(dst.Name())
	if err != nil {
		t.Errorf("ReadFile(%v) failed: %v", dst.Name(), err)
	}
	if bytes.Compare(data, output) != 0 {
		t.Errorf("Data mismatch:\nexpected %v %v\ngot %v %v", len(data), data[:100], len(output), output[:100])
	}
	jMediaInfo, err := ioutil.ReadFile(dst.Name() + ".__info")
	if err != nil {
		t.Errorf("ReadFile(%v) failed: %v", dst.Name()+".__info", err)
	}
	if expected := `{"Type":"application/octet-stream","Encoding":""}`; string(jMediaInfo) != expected {
		t.Errorf("unexpected media info: expected %q, got %q", expected, string(jMediaInfo))
	}
	if err := Delete(runtime.NewContext(), von); err != nil {
		t.Errorf("Delete(%v) failed: %v", von, err)
	}
}

// TestDownloadURL tests the binary repository client-side library
// DownloadURL method.
func TestDownloadURL(t *testing.T) {
	von, cleanup := setupRepository(t)
	defer cleanup()
	url, _, err := DownloadURL(runtime.NewContext(), von)
	if err != nil {
		t.Fatalf("DownloadURL(%v) failed: %v", von, err)
	}
	if got, want := url, "http://test-root-url/test"; got != want {
		t.Fatalf("unexpect output: got %v, want %v", got, want)
	}
}
