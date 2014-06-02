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
	"regexp"
	"sync"
	"syscall"

	"veyron2/ipc"
	"veyron2/services/mgmt/content"
	"veyron2/vlog"
)

// invoker holds the state of a content manager invocation.
type invoker struct {
	// root is the directory at which the content manager namespace is
	// mounted.
	root string
	// depth determines the depth of the directory hierarchy that the
	// content manager uses to organize the content in the local file
	// system. There is a trade-off here: smaller values lead to faster
	// access, while higher values allow the performance to scale to
	// large content collections. The number should be a value in
	// between 0 and (md5.Size - 1).
	//
	// Note that the cardinality of each level (except the leaf level)
	// is at most 256. If you expect to have X total content items, and
	// you want to limit directories to at most Y entries (because of
	// filesystem limitations), then you should set depth to at least
	// log_256(X/Y). For example, using hierarchyDepth = 3 with a local
	// filesystem that can handle up to 1,000 entries per directory
	// before its performance degrades allows the content manager to
	// store 16B objects.
	depth int
	// fs is a lock used to atomatically modify the contents of the
	// local filesystem used for storing the content.
	fs *sync.Mutex
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative veyron name to identify content.
	suffix string
}

var (
	errContentExists   = errors.New("content already exists")
	errMissingChecksum = errors.New("missing checksum")
	errInvalidChecksum = errors.New("invalid checksum")
	errInvalidSuffix   = errors.New("invalid suffix")
	errOperationFailed = errors.New("operation failed")
)

// newInvoker is the invoker factory.
func newInvoker(root string, depth int, fs *sync.Mutex, suffix string) *invoker {
	if min, max := 0, md5.Size-1; min > depth || depth > max {
		panic(fmt.Sprintf("Depth should be a value between %v and %v, got %v.", min, max, depth))
	}
	return &invoker{
		root:   root,
		depth:  depth,
		fs:     fs,
		suffix: suffix,
	}
}

var (
	reg = regexp.MustCompile("[a-f0-9]+")
)

func isValid(suffix string) bool {
	return reg.Match([]byte(suffix))
}

// CONTENT INTERFACE IMPLEMENTATION

const bufferLength = 1024

func (i *invoker) generateDir(checksum string) string {
	dir := ""
	for j := 0; j < i.depth; j++ {
		dir = filepath.Join(dir, checksum[j*2:(j+1)*2])
	}
	return dir
}

func (i *invoker) Delete(context ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Delete()", i.suffix)
	if !isValid(i.suffix) {
		return errInvalidSuffix
	}
	filename := filepath.Join(i.root, i.generateDir(i.suffix), i.suffix)
	i.fs.Lock()
	defer i.fs.Unlock()
	// Remove the content and all directories on the path back to the
	// root that are left empty after the content is removed.
	if err := os.Remove(filename); err != nil {
		return errOperationFailed
	}
	for {
		filename = filepath.Dir(filename)
		if i.root == filename {
			break
		}
		if err := os.Remove(filename); err != nil {
			if err.(*os.PathError).Err.Error() == syscall.ENOTEMPTY.Error() {
				break
			}
			return errOperationFailed
		}
	}
	return nil
}

func (i *invoker) Download(context ipc.ServerContext, stream content.ContentServiceDownloadStream) error {
	vlog.VI(0).Infof("%v.Download()", i.suffix)
	if !isValid(i.suffix) {
		return errInvalidSuffix
	}
	h := md5.New()
	file, err := os.Open(filepath.Join(i.root, i.generateDir(i.suffix), i.suffix))
	if err != nil {
		return errOperationFailed
	}
	defer file.Close()
	buffer := make([]byte, bufferLength)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return errOperationFailed
		}
		if n == 0 {
			break
		}
		if err := stream.Send(buffer[:n]); err != nil {
			return errOperationFailed
		}
		h.Write(buffer[:n])
	}
	if hex.EncodeToString(h.Sum(nil)) != i.suffix {
		return errInvalidChecksum
	}
	return nil
}

func (i *invoker) Upload(context ipc.ServerContext, stream content.ContentServiceUploadStream) (string, error) {
	vlog.VI(0).Infof("%v.Upload()", i.suffix)
	if i.suffix != "" {
		return "", errInvalidSuffix
	}
	h := md5.New()
	file, err := ioutil.TempFile(i.root, "")
	if err != nil {
		return "", errOperationFailed
	}
	defer file.Close()
	for {
		bytes, err := stream.Recv()
		if err != nil && err != io.EOF {
			return "", errOperationFailed
		}
		if err == io.EOF {
			break
		}
		if _, err := file.Write(bytes); err != nil {
			return "", errOperationFailed
		}
		h.Write(bytes)
	}
	checksum := hex.EncodeToString(h.Sum(nil))
	dir := filepath.Join(i.root, i.generateDir(checksum))
	i.fs.Lock()
	defer i.fs.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", errOperationFailed
	}
	filename := filepath.Join(dir, checksum)
	if _, err := os.Stat(filename); err != nil && !os.IsNotExist(err) {
		os.Remove(file.Name())
		return "", errContentExists
	}
	// Note that this rename operation requires only a metadata update
	// (as opposed to copy and delete) because file.Name() and filename
	// share the same mount point.
	if err := os.Rename(file.Name(), filename); err != nil {
		os.Remove(file.Name())
		return "", errOperationFailed
	}
	return checksum, nil
}
