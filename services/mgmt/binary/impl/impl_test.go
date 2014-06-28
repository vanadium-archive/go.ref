package impl

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/repository"
	"veyron2/vlog"
)

const (
	seedEnv      = "VEYRON_RNG_SEED"
	veyronPrefix = "veyron_binary_repository"
)

var (
	random []byte
	rnd    *rand.Rand
)

func init() {
	rt.Init()
	// Initialize pseudo-random number generator.
	seed := time.Now().UnixNano()
	seedString := os.Getenv(seedEnv)
	if seedString != "" {
		var err error
		base, bitSize := 0, 64
		seed, err = strconv.ParseInt(seedString, 0, 64)
		if err != nil {
			vlog.Fatalf("ParseInt(%v, %v, %v) failed: %v", seedString, base, bitSize, err)
		}
	}
	vlog.VI(0).Infof("Using pseudo-random number generator seed = %v", seed)
	rnd = rand.New(rand.NewSource(seed))
}

// invokeUpload invokes the Upload RPC using the given client binary
// <binary> and streams the given binary <binary> to it.
func invokeUpload(t *testing.T, binary repository.Binary, data []byte, part int32) error {
	stream, err := binary.Upload(rt.R().NewContext(), part)
	if err != nil {
		t.Errorf("Upload() failed: %v", err)
		return err
	}
	if err := stream.Send(data); err != nil {
		if err := stream.Finish(); err != nil {
			t.Logf("Finish() failed: %v", err)
		}
		t.Logf("Send() failed: %v", err)
		return err
	}
	if err := stream.CloseSend(); err != nil {
		if err := stream.Finish(); err != nil {
			t.Logf("Finish() failed: %v", err)
		}
		t.Logf("CloseSend() failed: %v", err)
		return err
	}
	if err := stream.Finish(); err != nil {
		t.Logf("Finish() failed: %v", err)
		return err
	}
	return nil
}

// invokeDownload invokes the Download RPC using the given client binary
// <binary> and streams binary from to it.
func invokeDownload(t *testing.T, binary repository.Binary, part int32) ([]byte, error) {
	stream, err := binary.Download(rt.R().NewContext(), part)
	if err != nil {
		t.Errorf("Download() failed: %v", err)
		return nil, err
	}
	output := make([]byte, 0)
	for {
		bytes, err := stream.Recv()
		if err != nil && err != io.EOF {
			if err := stream.Finish(); err != nil {
				t.Logf("Finish() failed: %v", err)
			}
			t.Logf("Recv() failed: %v", err)
			return nil, err
		}
		if err == io.EOF {
			break
		}
		output = append(output, bytes...)
	}
	if err := stream.Finish(); err != nil {
		t.Logf("Finish() failed: %v", err)
		return nil, err
	}
	return output, nil
}

func generateBits(size int) []byte {
	buffer := make([]byte, size)
	offset := 0
	for {
		bits := int64(rnd.Int63())
		for i := 0; i < 8; i++ {
			buffer[offset] = byte(bits & 0xff)
			size--
			if size == 0 {
				return buffer
			}
			offset++
			bits >>= 8
		}
	}
}

func randomBytes(size int) []byte {
	buffer := make([]byte, size)
	// Generate a 4MB of random bytes since that is a value commonly
	// used in this test.
	if len(random) == 0 {
		random = generateBits(4 << 20)
	}
	if size > len(random) {
		extra := generateBits(size - len(random))
		random = append(random, extra...)
	}
	start := rnd.Intn(len(random) - size + 1)
	copy(buffer, random[start:start+size])
	return buffer
}

// startServer starts the binary repository server.
func startServer(t *testing.T, depth int) (repository.Binary, func()) {
	// Setup the root of the binary repository.
	root, err := ioutil.TempDir("", veyronPrefix)
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	path, perm := filepath.Join(root, VersionFile), os.FileMode(0600)
	if err := ioutil.WriteFile(path, []byte(Version), perm); err != nil {
		vlog.Fatalf("WriteFile(%v, %v, %v) failed: %v", path, Version, perm, err)
	}
	// Setup and start the binary repository server.
	server, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	dispatcher, err := NewDispatcher(root, depth, nil)
	if err != nil {
		t.Fatalf("NewDispatcher(%v, %v, %v) failed: %v", root, depth, nil, err)
	}
	suffix := ""
	if err := server.Register(suffix, dispatcher); err != nil {
		t.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	name := naming.JoinAddressName(endpoint.String(), "//test")
	binary, err := repository.BindBinary(name)
	if err != nil {
		t.Fatalf("BindRepository() failed: %v", err)
	}
	return binary, func() {
		// Shutdown the binary repository server.
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatalf("Remove(%v) failed: %v", path, err)
		}
		// Check that any directories and files that were created to
		// represent the binary objects have been garbage collected.
		if err := os.Remove(root); err != nil {
			t.Fatalf("Remove(%v) failed: %v", root, err)
		}
	}
}

// TestHierarchy checks that the binary repository works correctly for
// all possible valid values of the depth used for the directory
// hierarchy that stores binary objects in the local file system.
func TestHierarchy(t *testing.T) {
	for i := 0; i < md5.Size; i++ {
		binary, cleanup := startServer(t, i)
		defer cleanup()
		// Create up to 4MB of random bytes.
		size := rnd.Intn(1000 * bufferLength)
		data := randomBytes(size)
		// Test the binary repository interface.
		if err := binary.Create(rt.R().NewContext(), 1); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
		if err := invokeUpload(t, binary, data, 0); err != nil {
			t.FailNow()
		}
		parts, err := binary.Stat(rt.R().NewContext())
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
		output, err := invokeDownload(t, binary, 0)
		if err != nil {
			t.FailNow()
		}
		if bytes.Compare(output, data) != 0 {
			t.Fatalf("Unexpected output: expected %v, got %v", data, output)
		}
		if err := binary.Delete(rt.R().NewContext()); err != nil {
			t.Fatalf("Delete() failed: %v", err)
		}
	}
}

// TestMultiPart checks that the binary repository supports multi-part
// uploads and downloads ranging the number of parts the test binary
// consists of.
func TestMultiPart(t *testing.T) {
	for length := 2; length < 5; length++ {
		binary, cleanup := startServer(t, 2)
		defer cleanup()
		// Create <length> chunks of up to 4MB of random bytes.
		data := make([][]byte, length)
		for i := 0; i < length; i++ {
			size := rnd.Intn(1000 * bufferLength)
			data[i] = randomBytes(size)
		}
		// Test the binary repository interface.
		if err := binary.Create(rt.R().NewContext(), int32(length)); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
		for i := 0; i < length; i++ {
			if err := invokeUpload(t, binary, data[i], int32(i)); err != nil {
				t.FailNow()
			}
		}
		parts, err := binary.Stat(rt.R().NewContext())
		if err != nil {
			t.Fatalf("Stat() failed: %v", err)
		}
		h := md5.New()
		for i := 0; i < length; i++ {
			hpart := md5.New()
			output, err := invokeDownload(t, binary, int32(i))
			if err != nil {
				t.FailNow()
			}
			if bytes.Compare(output, data[i]) != 0 {
				t.Fatalf("Unexpected output: expected %v, got %v", data[i], output)
			}
			h.Write(data[i])
			hpart.Write(data[i])
			checksum := hex.EncodeToString(hpart.Sum(nil))
			if expected, got := checksum, parts[i].Checksum; expected != got {
				t.Fatalf("Unexpected checksum: expected %v, got %v", expected, got)
			}
			if expected, got := len(data[i]), int(parts[i].Size); expected != got {
				t.Fatalf("Unexpected size: expected %v, got %v", expected, got)
			}
		}
		if err := binary.Delete(rt.R().NewContext()); err != nil {
			t.Fatalf("Delete() failed: %v", err)
		}
	}
}

// TestResumption checks that the binary interface supports upload
// resumption ranging the number of parts the uploaded binary consists
// of.
func TestResumption(t *testing.T) {
	for length := 2; length < 5; length++ {
		binary, cleanup := startServer(t, 2)
		defer cleanup()
		// Create <length> chunks of up to 4MB of random bytes.
		data := make([][]byte, length)
		for i := 0; i < length; i++ {
			size := rnd.Intn(1000 * bufferLength)
			data[i] = randomBytes(size)
		}
		if err := binary.Create(rt.R().NewContext(), int32(length)); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
		// Simulate a flaky upload client that keeps uploading parts until
		// finished.
		for {
			parts, err := binary.Stat(rt.R().NewContext())
			if err != nil {
				t.Fatalf("Stat() failed: %v", err)
			}
			finished := true
			for _, part := range parts {
				finished = finished && (part != MissingPart)
			}
			if finished {
				break
			}
			for i := 0; i < length; i++ {
				fail := rnd.Intn(2)
				if parts[i] == MissingPart && fail != 0 {
					if err := invokeUpload(t, binary, data[i], int32(i)); err != nil {
						t.FailNow()
					}
				}
			}
		}
		if err := binary.Delete(rt.R().NewContext()); err != nil {
			t.Fatalf("Delete() failed: %v", err)
		}
	}
}

// TestErrors checks that the binary interface correctly reports errors.
func TestErrors(t *testing.T) {
	binary, cleanup := startServer(t, 2)
	defer cleanup()
	length := 2
	data := make([][]byte, length)
	for i := 0; i < length; i++ {
		size := rnd.Intn(1000 * bufferLength)
		data[i] = make([]byte, size)
		for j := 0; j < size; j++ {
			data[i][j] = byte(rnd.Int())
		}
	}
	if err := binary.Create(rt.R().NewContext(), int32(length)); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if err := binary.Create(rt.R().NewContext(), int32(length)); err == nil {
		t.Fatalf("Create() did not fail when it should have")
	}
	if err := invokeUpload(t, binary, data[0], 0); err != nil {
		t.Fatalf("Upload() failed: %v", err)
	}
	if err := invokeUpload(t, binary, data[0], 0); err == nil {
		t.Fatalf("Upload() did not fail when it should have")
	}
	if _, err := invokeDownload(t, binary, 1); err == nil {
		t.Fatalf("Download() did not fail when it should have")
	}
	if err := invokeUpload(t, binary, data[1], 1); err != nil {
		t.Fatalf("Upload() failed: %v", err)
	}
	if _, err := invokeDownload(t, binary, 0); err != nil {
		t.Fatalf("Download() failed: %v", err)
	}
	if err := binary.Delete(rt.R().NewContext()); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}
	if err := binary.Delete(rt.R().NewContext()); err == nil {
		t.Fatalf("Delete() did not fail when it should have")
	}
}
