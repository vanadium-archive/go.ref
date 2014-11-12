package impl

import (
	"io"
	"math"
	"os"
	"path/filepath"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/services/mgmt/logreader/types"
	"veyron.io/veyron/veyron2/vlog"
)

// logFileInvoker holds the state of a logfile invocation.
type logFileInvoker struct {
	// root is the root directory from which the object names are based.
	root string
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative object name to identify a log file.
	suffix string
}

// NewLogFileServer returns a new log file server.
func NewLogFileServer(root, suffix string) interface{} {
	return &logFileInvoker{filepath.Clean(root), suffix}
}

// Size returns the size of the log file, in bytes.
func (i *logFileInvoker) Size(call ipc.ServerCall) (int64, error) {
	vlog.VI(1).Infof("%v.Size()", i.suffix)
	fname, err := translateNameToFilename(i.root, i.suffix)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(fname)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, errNotFound
		}
		vlog.Errorf("Stat(%v) failed: %v", fname, err)
		return 0, errOperationFailed
	}
	if fi.IsDir() {
		return 0, errOperationFailed
	}
	return fi.Size(), nil
}

// ReadLog returns log entries from the log file.
func (i *logFileInvoker) ReadLog(call ipc.ServerCall, startpos int64, numEntries int32, follow bool) (int64, error) {
	vlog.VI(1).Infof("%v.ReadLog(%v, %v, %v)", i.suffix, startpos, numEntries, follow)
	fname, err := translateNameToFilename(i.root, i.suffix)
	if err != nil {
		return 0, err
	}
	f, err := os.Open(fname)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, errNotFound
		}
		return 0, errOperationFailed
	}
	reader := newFollowReader(call, f, startpos, follow)
	if numEntries == types.AllEntries {
		numEntries = int32(math.MaxInt32)
	}
	for n := int32(0); n < numEntries; n++ {
		line, offset, err := reader.readLine()
		if err == io.EOF && n > 0 {
			return reader.tell(), nil
		}
		if err == io.EOF {
			return reader.tell(), errEOF
		}
		if err != nil {
			return reader.tell(), errOperationFailed
		}
		if err := call.Send(types.LogEntry{Position: offset, Line: line}); err != nil {
			return reader.tell(), err
		}
	}
	return reader.tell(), nil
}

// VGlobChildren returns the list of files in a directory, or an empty list if
// the object is a file.
func (i *logFileInvoker) VGlobChildren() ([]string, error) {
	vlog.VI(1).Infof("%v.VGlobChildren()", i.suffix)
	dirName, err := translateNameToFilename(i.root, i.suffix)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(dirName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errNotFound
		}
		return nil, errOperationFailed
	}
	if !stat.IsDir() {
		return nil, nil
	}

	f, err := os.Open(dirName)
	if err != nil {
		return nil, errOperationFailed
	}
	fi, err := f.Readdir(0)
	if err != nil {
		return nil, errOperationFailed
	}
	f.Close()
	children := []string{}
	for _, file := range fi {
		fileName := file.Name()
		if fileName == "." || fileName == ".." {
			continue
		}
		children = append(children, file.Name())
	}
	return children, nil
}
