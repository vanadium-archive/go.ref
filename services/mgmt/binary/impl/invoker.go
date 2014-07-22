// The implementation of the binary repository interface stores
// objects identified by object name suffixes using the local file
// system. Given an object name suffix, the implementation computes an
// MD5 hash of the suffix and generates the following path in the
// local filesystem: /<root>/<dir_1>/.../<dir_n>/<hash>. The root and
// the directory depth are parameters of the implementation. The
// contents of the directory include the checksum and data for the
// object and each of its individual parts:
//
// checksum
// data
// <part_1>/checksum
// <part_1>/data
// ...
// <part_n>/checksum
// <part_n>/data
//
// TODO(jsimsa): Add an "fsck" method that cleans up existing on-disk
// repository and provide a command-line flag that identifies whether
// fsck should run when new repository server process starts up.
package impl

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"veyron2/ipc"
	"veyron2/services/mgmt/binary"
	"veyron2/services/mgmt/repository"
	"veyron2/vlog"
)

// state holds the state shared across different binary repository
// invocations.
type state struct {
	// depth determines the depth of the directory hierarchy that the
	// binary repository uses to organize binaries in the local file
	// system. There is a trade-off here: smaller values lead to faster
	// access, while higher values allow the performance to scale to
	// larger collections of binaries. The number should be a value
	// between 0 and (md5.Size - 1).
	//
	// Note that the cardinality of each level (except the leaf level)
	// is at most 256. If you expect to have X total binary items, and
	// you want to limit directories to at most Y entries (because of
	// filesystem limitations), then you should set depth to at least
	// log_256(X/Y). For example, using hierarchyDepth = 3 with a local
	// filesystem that can handle up to 1,000 entries per directory
	// before its performance degrades allows the binary repository to
	// store 16B objects.
	depth int
	// root identifies the local filesystem directory in which the
	// binary repository stores its objects.
	root string
}

// invoker holds the state of a binary repository invocation.
type invoker struct {
	// path is the local filesystem path to the object identified by the
	// object name suffix.
	path string
	// state holds the state shared across different binary repository
	// invocations.
	state *state
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative object name to identify a binary.
	suffix string
}

const (
	checksum = "checksum"
	data     = "data"
	lock     = "lock"
)

var (
	errExist           = errors.New("binary already exists")
	errNotExist        = errors.New("binary does not exist")
	errInProgress      = errors.New("identical upload already in progress")
	errInvalidParts    = errors.New("invalid number of parts")
	errOperationFailed = errors.New("operation failed")
)

// TODO(jsimsa): When VDL supports composite literal constants, remove
// this definition.
var MissingPart = binary.PartInfo{
	Checksum: binary.MissingChecksum,
	Size:     binary.MissingSize,
}

// newInvoker is the invoker factory.
func newInvoker(state *state, suffix string) *invoker {
	// Generate the local filesystem path for the object identified by
	// the object name suffix.
	h := md5.New()
	h.Write([]byte(suffix))
	hash := hex.EncodeToString(h.Sum(nil))
	dir := ""
	for j := 0; j < state.depth; j++ {
		dir = filepath.Join(dir, hash[j*2:(j+1)*2])
	}
	path := filepath.Join(state.root, dir, hash)
	return &invoker{
		path:   path,
		state:  state,
		suffix: suffix,
	}
}

// BINARY INTERFACE IMPLEMENTATION

const bufferLength = 4096

// consolidate checks if all parts of a binary have been uploaded and
// if so, creates the aggregate binary and checksum.
func (i *invoker) consolidate() error {
	// Coordinate concurrent uploaders to make sure the aggregate binary
	// and checksum is created only once.
	err := i.checksumExists(i.path)
	switch err {
	case nil:
		return nil
	case errNotExist:
	default:
		return err
	}
	// Check if all parts have been uploaded.
	parts, err := i.getParts()
	if err != nil {
		return err
	}
	for _, part := range parts {
		err := i.checksumExists(part)
		switch err {
		case errNotExist:
			return nil
		case nil:
		default:
			return err
		}
	}
	// Create the aggregate binary and its checksum.
	suffix := ""
	output, err := ioutil.TempFile(i.path, suffix)
	if err != nil {
		vlog.Errorf("TempFile(%v, %v) failed: %v", i.path, suffix, err)
		return errOperationFailed
	}
	defer output.Close()
	buffer, h := make([]byte, bufferLength), md5.New()
	for _, part := range parts {
		input, err := os.Open(filepath.Join(part, data))
		if err != nil {
			vlog.Errorf("Open(%v) failed: %v", filepath.Join(part, data), err)
			if err := os.Remove(output.Name()); err != nil {
				vlog.Errorf("Remove(%v) failed: %v", output.Name(), err)
			}
			return errOperationFailed
		}
		defer input.Close()
		for {
			n, err := input.Read(buffer)
			if err != nil && err != io.EOF {
				vlog.Errorf("Read() failed: %v", err)
				if err := os.Remove(output.Name()); err != nil {
					vlog.Errorf("Remove(%v) failed: %v", output.Name(), err)
				}
				return errOperationFailed
			}
			if n == 0 {
				break
			}
			if _, err := output.Write(buffer[:n]); err != nil {
				vlog.Errorf("Write() failed: %v", err)
				if err := os.Remove(output.Name()); err != nil {
					vlog.Errorf("Remove(%v) failed: %v", output.Name(), err)
				}
				return errOperationFailed
			}
			h.Write(buffer[:n])
		}
	}
	hash := hex.EncodeToString(h.Sum(nil))
	checksumFile, perm := filepath.Join(i.path, checksum), os.FileMode(0600)
	if err := ioutil.WriteFile(checksumFile, []byte(hash), perm); err != nil {
		vlog.Errorf("WriteFile(%v, %v, %v) failed: %v", checksumFile, hash, perm, err)
		if err := os.Remove(output.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", output.Name(), err)
		}
		return errOperationFailed
	}
	dataFile := filepath.Join(i.path, data)
	if err := os.Rename(output.Name(), dataFile); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", output.Name(), dataFile, err)
		if err := os.Remove(output.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", output.Name(), err)
		}
		return errOperationFailed
	}
	return nil
}

// checksumExists checks whether the given path contains a
// checksum. The implementation uses the existence of checksum to
// determine whether the binary (part) identified by the given path
// exists.
func (i *invoker) checksumExists(path string) error {
	checksumFile := filepath.Join(path, checksum)
	_, err := os.Stat(checksumFile)
	switch {
	case os.IsNotExist(err):
		return errNotExist
	case err != nil:
		vlog.Errorf("Stat(%v) failed: %v", path, err)
		return errOperationFailed
	default:
		return nil
	}
}

// generatePartPath generates a path for the given binary part.
func (i *invoker) generatePartPath(part int) string {
	return filepath.Join(i.path, fmt.Sprintf("%d", part))
}

// getParts returns a collection of paths to the parts of the binary.
func (i *invoker) getParts() ([]string, error) {
	infos, err := ioutil.ReadDir(i.path)
	if err != nil {
		vlog.Errorf("ReadDir(%v) failed: %v", i.path, err)
		return []string{}, errOperationFailed
	}
	n := 0
	result := make([]string, len(infos))
	for _, info := range infos {
		if info.IsDir() {
			idx, err := strconv.Atoi(info.Name())
			if err != nil {
				vlog.Errorf("Atoi(%v) failed: %v", info.Name(), err)
				return []string{}, errOperationFailed
			}
			result[idx] = filepath.Join(i.path, info.Name())
			n++
		}
	}
	result = result[:n]
	return result, nil
}

func (i *invoker) Create(_ ipc.ServerContext, nparts int32) error {
	vlog.Infof("%v.Create(%v)", i.suffix, nparts)
	if nparts < 1 {
		return errInvalidParts
	}
	parent, perm := filepath.Dir(i.path), os.FileMode(0700)
	if err := os.MkdirAll(parent, perm); err != nil {
		vlog.Errorf("MkdirAll(%v, %v) failed: %v", parent, perm, err)
		return errOperationFailed
	}
	prefix := "creating-"
	tmpDir, err := ioutil.TempDir(parent, prefix)
	if err != nil {
		vlog.Errorf("TempDir(%v, %v) failed: %v", parent, prefix, err)
		return errOperationFailed
	}
	for j := 0; j < int(nparts); j++ {
		partPath, partPerm := filepath.Join(tmpDir, fmt.Sprintf("%d", j)), os.FileMode(0700)
		if err := os.MkdirAll(partPath, partPerm); err != nil {
			vlog.Errorf("MkdirAll(%v, %v) failed: %v", partPath, partPerm, err)
			if err := os.RemoveAll(tmpDir); err != nil {
				vlog.Errorf("RemoveAll(%v) failed: %v", tmpDir, err)
			}
			return errOperationFailed
		}
	}
	// Use os.Rename() to atomically create the binary directory
	// structure.
	if err := os.Rename(tmpDir, i.path); err != nil {
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				vlog.Errorf("RemoveAll(%v) failed: %v", tmpDir, err)
			}
		}()
		if os.IsExist(err) {
			return errExist
		}
		vlog.Errorf("Rename(%v, %v) failed: %v", tmpDir, i.path, err)
		return errOperationFailed
	}
	return nil
}

func (i *invoker) Delete(context ipc.ServerContext) error {
	vlog.Infof("%v.Delete()", i.suffix)
	if _, err := os.Stat(i.path); err != nil {
		if os.IsNotExist(err) {
			return errNotExist
		}
		vlog.Errorf("Stat(%v) failed: %v", i.path, err)
		return errOperationFailed
	}
	// Use os.Rename() to atomically remove the binary directory
	// structure.
	path := filepath.Join(filepath.Dir(i.path), "removing-"+filepath.Base(i.path))
	if err := os.Rename(i.path, path); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", i.path, path, err)
		return errOperationFailed
	}
	if err := os.RemoveAll(path); err != nil {
		vlog.Errorf("Remove(%v) failed: %v", path, err)
		return errOperationFailed
	}
	for {
		// Remove the binary and all directories on the path back to the
		// root that are left empty after the binary is removed.
		path = filepath.Dir(path)
		if i.state.root == path {
			break
		}
		if err := os.Remove(path); err != nil {
			if err.(*os.PathError).Err.Error() == syscall.ENOTEMPTY.Error() {
				break
			}
			vlog.Errorf("Remove(%v) failed: %v", path, err)
			return errOperationFailed
		}
	}
	return nil
}

func (i *invoker) Download(context ipc.ServerContext, part int32, stream repository.BinaryServiceDownloadStream) error {
	vlog.Infof("%v.Download(%v)", i.suffix, part)
	path := i.generatePartPath(int(part))
	if err := i.checksumExists(path); err != nil {
		return err
	}
	dataPath := filepath.Join(path, data)
	file, err := os.Open(dataPath)
	if err != nil {
		vlog.Errorf("Open(%v) failed: %v", path, err)
		return errOperationFailed
	}
	defer file.Close()
	buffer := make([]byte, bufferLength)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			vlog.Errorf("Read() failed: %v", err)
			return errOperationFailed
		}
		if n == 0 {
			break
		}
		if err := stream.Send(buffer[:n]); err != nil {
			vlog.Errorf("Send() failed: %v", err)
			return errOperationFailed
		}
	}
	return nil
}

func (i *invoker) DownloadURL(ipc.ServerContext) (string, int64, error) {
	vlog.Infof("%v.DownloadURL()", i.suffix)
	// TODO(jsimsa): Implement.
	return "", 0, nil
}

func (i *invoker) Stat(ipc.ServerContext) ([]binary.PartInfo, error) {
	vlog.Infof("%v.Stat()", i.suffix)
	result := make([]binary.PartInfo, 0)
	parts, err := i.getParts()
	if err != nil {
		return []binary.PartInfo{}, err
	}
	for _, part := range parts {
		checksumFile := filepath.Join(part, checksum)
		bytes, err := ioutil.ReadFile(checksumFile)
		if err != nil {
			if os.IsNotExist(err) {
				result = append(result, MissingPart)
				continue
			}
			vlog.Errorf("ReadFile(%v) failed: %v", checksumFile, err)
			return []binary.PartInfo{}, errOperationFailed
		}
		dataFile := filepath.Join(part, data)
		fi, err := os.Stat(dataFile)
		if err != nil {
			if os.IsNotExist(err) {
				result = append(result, MissingPart)
				continue
			}
			vlog.Errorf("Stat(%v) failed: %v", dataFile, err)
			return []binary.PartInfo{}, errOperationFailed
		}
		result = append(result, binary.PartInfo{Checksum: string(bytes), Size: fi.Size()})
	}
	return result, nil
}

func (i *invoker) Upload(context ipc.ServerContext, part int32, stream repository.BinaryServiceUploadStream) error {
	vlog.Infof("%v.Upload(%v)", i.suffix, part)
	path, suffix := i.generatePartPath(int(part)), ""
	err := i.checksumExists(path)
	switch err {
	case nil:
		return errExist
	case errNotExist:
	default:
		return err
	}
	// Use os.OpenFile() to resolve races.
	lockPath, flags, perm := filepath.Join(path, lock), os.O_CREATE|os.O_WRONLY|os.O_EXCL, os.FileMode(0600)
	lockFile, err := os.OpenFile(lockPath, flags, perm)
	if err != nil {
		if os.IsExist(err) {
			return errInProgress
		}
		vlog.Errorf("OpenFile(%v, %v, %v) failed: %v", lockPath, flags, suffix, err)
		return errOperationFailed
	}
	defer os.Remove(lockFile.Name())
	defer lockFile.Close()
	file, err := ioutil.TempFile(path, suffix)
	if err != nil {
		vlog.Errorf("TempFile(%v, %v) failed: %v", path, suffix, err)
		return errOperationFailed
	}
	defer file.Close()
	h := md5.New()
	for stream.Advance() {
		bytes := stream.Value()
		if _, err := file.Write(bytes); err != nil {
			vlog.Errorf("Write() failed: %v", err)
			if err := os.Remove(file.Name()); err != nil {
				vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
			}
			return errOperationFailed
		}
		h.Write(bytes)
	}

	if err := stream.Err(); err != nil {
		vlog.Errorf("Recv() failed: %v", err)
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return errOperationFailed
	}

	hash := hex.EncodeToString(h.Sum(nil))
	checksumFile, perm := filepath.Join(path, checksum), os.FileMode(0600)
	if err := ioutil.WriteFile(checksumFile, []byte(hash), perm); err != nil {
		vlog.Errorf("WriteFile(%v, %v, %v) failed: %v", checksumFile, hash, perm, err)
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return errOperationFailed
	}
	dataFile := filepath.Join(path, data)
	if err := os.Rename(file.Name(), dataFile); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", file.Name(), dataFile, err)
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return errOperationFailed
	}
	return nil
}
