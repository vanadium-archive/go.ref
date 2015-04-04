// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package binarylib_test

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/repository"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	_ "v.io/x/ref/profiles/static"
	"v.io/x/ref/services/binary/binarylib"
	mgmttest "v.io/x/ref/services/mgmt/lib/testutil"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

const (
	v23Prefix = "vanadium_binary_repository"
)

// startServer starts the binary repository server.
func startServer(t *testing.T, ctx *context.T, depth int) (repository.BinaryClientMethods, string, string, func()) {
	// Setup the root of the binary repository.
	rootDir, cleanup := mgmttest.SetupRootDir(t, "bindir")
	prepDirectory(t, rootDir)

	// Setup and start the binary repository server.
	server, endpoint := mgmttest.NewServer(ctx)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	state, err := binarylib.NewState(rootDir, listener.Addr().String(), depth)
	if err != nil {
		t.Fatalf("NewState(%v, %v) failed: %v", rootDir, listener.Addr().String(), depth, err)
	}
	go func() {
		if err := http.Serve(listener, http.FileServer(binarylib.NewHTTPRoot(state))); err != nil {
			vlog.Fatalf("Serve() failed: %v", err)
		}
	}()
	dispatcher, err := binarylib.NewDispatcher(v23.GetPrincipal(ctx), state)
	if err != nil {
		t.Fatalf("NewDispatcher failed: %v", err)
	}
	dontPublishName := ""
	if err := server.ServeDispatcher(dontPublishName, dispatcher); err != nil {
		t.Fatalf("Serve(%q) failed: %v", dontPublishName, err)
	}
	name := naming.JoinAddressName(endpoint, "test")
	binary := repository.BinaryClient(name)
	return binary, endpoint, fmt.Sprintf("http://%s/test", listener.Addr()), func() {
		// Shutdown the binary repository server.
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
		cleanup()
	}
}

// TestHierarchy checks that the binary repository works correctly for
// all possible valid values of the depth used for the directory
// hierarchy that stores binary objects in the local file system.
func TestHierarchy(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	for i := 0; i < md5.Size; i++ {
		binary, ep, _, cleanup := startServer(t, ctx, i)
		defer cleanup()
		data := testData()

		// Test the binary repository interface.
		if err := binary.Create(ctx, 1, repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
		if streamErr, err := invokeUpload(t, ctx, binary, data, 0); streamErr != nil || err != nil {
			t.FailNow()
		}
		parts, _, err := binary.Stat(ctx)
		if err != nil {
			t.Fatalf("Stat() failed: %v", err)
		}
		h := md5.New()
		h.Write(data)
		checksum := hex.EncodeToString(h.Sum(nil))
		if expected, got := checksum, parts[0].Checksum; expected != got {
			t.Fatalf("Unexpected checksum: expected %v, got %v", expected, got)
		}
		if expected, got := len(data), int(parts[0].Size); expected != got {
			t.Fatalf("Unexpected size: expected %v, got %v", expected, got)
		}
		output, streamErr, err := invokeDownload(t, ctx, binary, 0)
		if streamErr != nil || err != nil {
			t.FailNow()
		}
		if bytes.Compare(output, data) != 0 {
			t.Fatalf("Unexpected output: expected %v, got %v", data, output)
		}
		results, _, err := testutil.GlobName(ctx, naming.JoinAddressName(ep, ""), "...")
		if err != nil {
			t.Fatalf("GlobName failed: %v", err)
		}
		if expected := []string{"", "test"}; !reflect.DeepEqual(results, expected) {
			t.Errorf("Unexpected results: expected %q, got %q", expected, results)
		}
		if err := binary.Delete(ctx); err != nil {
			t.Fatalf("Delete() failed: %v", err)
		}
	}
}

// TestMultiPart checks that the binary repository supports multi-part
// uploads and downloads ranging the number of parts the test binary
// consists of.
func TestMultiPart(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	for length := 2; length < 5; length++ {
		binary, _, _, cleanup := startServer(t, ctx, 2)
		defer cleanup()
		// Create <length> chunks of up to 4MB of random bytes.
		data := make([][]byte, length)
		for i := 0; i < length; i++ {
			data[i] = testData()
		}
		// Test the binary repository interface.
		if err := binary.Create(ctx, int32(length), repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
		for i := 0; i < length; i++ {
			if streamErr, err := invokeUpload(t, ctx, binary, data[i], int32(i)); streamErr != nil || err != nil {
				t.FailNow()
			}
		}
		parts, _, err := binary.Stat(ctx)
		if err != nil {
			t.Fatalf("Stat() failed: %v", err)
		}
		for i := 0; i < length; i++ {
			hpart := md5.New()
			output, streamErr, err := invokeDownload(t, ctx, binary, int32(i))
			if streamErr != nil || err != nil {
				t.FailNow()
			}
			if bytes.Compare(output, data[i]) != 0 {
				t.Fatalf("Unexpected output: expected %v, got %v", data[i], output)
			}
			hpart.Write(data[i])
			checksum := hex.EncodeToString(hpart.Sum(nil))
			if expected, got := checksum, parts[i].Checksum; expected != got {
				t.Fatalf("Unexpected checksum: expected %v, got %v", expected, got)
			}
			if expected, got := len(data[i]), int(parts[i].Size); expected != got {
				t.Fatalf("Unexpected size: expected %v, got %v", expected, got)
			}
		}
		if err := binary.Delete(ctx); err != nil {
			t.Fatalf("Delete() failed: %v", err)
		}
	}
}

// TestResumption checks that the binary interface supports upload
// resumption ranging the number of parts the uploaded binary consists
// of.
func TestResumption(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	for length := 2; length < 5; length++ {
		binary, _, _, cleanup := startServer(t, ctx, 2)
		defer cleanup()
		// Create <length> chunks of up to 4MB of random bytes.
		data := make([][]byte, length)
		for i := 0; i < length; i++ {
			data[i] = testData()
		}
		if err := binary.Create(ctx, int32(length), repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
		// Simulate a flaky upload client that keeps uploading parts until
		// finished.
		for {
			parts, _, err := binary.Stat(ctx)
			if err != nil {
				t.Fatalf("Stat() failed: %v", err)
			}
			finished := true
			for _, part := range parts {
				finished = finished && (part != binarylib.MissingPart)
			}
			if finished {
				break
			}
			for i := 0; i < length; i++ {
				fail := testutil.Intn(2)
				if parts[i] == binarylib.MissingPart && fail != 0 {
					if streamErr, err := invokeUpload(t, ctx, binary, data[i], int32(i)); streamErr != nil || err != nil {
						t.FailNow()
					}
				}
			}
		}
		if err := binary.Delete(ctx); err != nil {
			t.Fatalf("Delete() failed: %v", err)
		}
	}
}

// TestErrors checks that the binary interface correctly reports errors.
func TestErrors(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	binary, _, _, cleanup := startServer(t, ctx, 2)
	defer cleanup()
	const length = 2
	data := make([][]byte, length)
	for i := 0; i < length; i++ {
		data[i] = testData()
		for j := 0; j < len(data[i]); j++ {
			data[i][j] = byte(testutil.Int())
		}
	}
	if err := binary.Create(ctx, int32(length), repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if err := binary.Create(ctx, int32(length), repository.MediaInfo{Type: "application/octet-stream"}); err == nil {
		t.Fatalf("Create() did not fail when it should have")
	} else if want := verror.ErrExist.ID; verror.ErrorID(err) != want {
		t.Fatalf("Unexpected error: %v, expected error id %v", err, want)
	}
	if streamErr, err := invokeUpload(t, ctx, binary, data[0], 0); streamErr != nil || err != nil {
		t.Fatalf("Upload() failed: %v", err)
	}
	if _, err := invokeUpload(t, ctx, binary, data[0], 0); err == nil {
		t.Fatalf("Upload() did not fail when it should have")
	} else if want := verror.ErrExist.ID; verror.ErrorID(err) != want {
		t.Fatalf("Unexpected error: %v, expected error id %v", err, want)
	}
	if _, _, err := invokeDownload(t, ctx, binary, 1); err == nil {
		t.Fatalf("Download() did not fail when it should have")
	} else if want := verror.ErrNoExist.ID; verror.ErrorID(err) != want {
		t.Fatalf("Unexpected error: %v, expected error id %v", err, want)
	}
	if streamErr, err := invokeUpload(t, ctx, binary, data[1], 1); streamErr != nil || err != nil {
		t.Fatalf("Upload() failed: %v", err)
	}
	if _, streamErr, err := invokeDownload(t, ctx, binary, 0); streamErr != nil || err != nil {
		t.Fatalf("Download() failed: %v", err)
	}
	// Upload/Download on a part number that's outside the range set forth in
	// Create should fail.
	for _, part := range []int32{-1, length} {
		if _, err := invokeUpload(t, ctx, binary, []byte("dummy"), part); err == nil {
			t.Fatalf("Upload() did not fail when it should have")
		} else if want := binarylib.ErrInvalidPart.ID; verror.ErrorID(err) != want {
			t.Fatalf("Unexpected error: %v, expected error id %v", err, want)
		}
		if _, _, err := invokeDownload(t, ctx, binary, part); err == nil {
			t.Fatalf("Download() did not fail when it should have")
		} else if want := binarylib.ErrInvalidPart.ID; verror.ErrorID(err) != want {
			t.Fatalf("Unexpected error: %v, expected error id %v", err, want)
		}
	}
	if err := binary.Delete(ctx); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}
	if err := binary.Delete(ctx); err == nil {
		t.Fatalf("Delete() did not fail when it should have")
	} else if want := verror.ErrNoExist.ID; verror.ErrorID(err) != want {
		t.Fatalf("Unexpected error: %v, expected error id %v", err, want)
	}
}

func TestGlob(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	_, ep, _, cleanup := startServer(t, ctx, 2)
	defer cleanup()
	data := testData()

	objects := []string{"foo", "bar", "hello world", "a/b/c"}
	for _, obj := range objects {
		name := naming.JoinAddressName(ep, obj)
		binary := repository.BinaryClient(name)

		if err := binary.Create(ctx, 1, repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
		if streamErr, err := invokeUpload(t, ctx, binary, data, 0); streamErr != nil || err != nil {
			t.FailNow()
		}
	}
	results, _, err := testutil.GlobName(ctx, naming.JoinAddressName(ep, ""), "...")
	if err != nil {
		t.Fatalf("GlobName failed: %v", err)
	}
	expected := []string{"", "a", "a/b", "a/b/c", "bar", "foo", "hello world"}
	if !reflect.DeepEqual(results, expected) {
		t.Errorf("Unexpected results: expected %q, got %q", expected, results)
	}
}
