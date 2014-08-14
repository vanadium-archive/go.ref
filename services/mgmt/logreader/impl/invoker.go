// Package impl implements the LogFile interface from
// veyron2/services/mgmt/logreader. It can be used to allow remote access to
// log files.
package impl

import (
	"io"
	"math"
	"os"
	"path"
	"strings"

	"veyron2/ipc"
	"veyron2/services/mgmt/logreader"
	"veyron2/verror"
	"veyron2/vlog"
)

// invoker holds the state of a logfile invocation.
type invoker struct {
	// root is the root directory from which the object names are based.
	root string
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative object name to identify a log file.
	suffix string
}

var (
	errCanceled        = verror.Abortedf("operation canceled")
	errNotFound        = verror.NotFoundf("log file not found")
	errEOF             = verror.Make(logreader.EOF, "EOF")
	errOperationFailed = verror.Internalf("operation failed")
)

// NewInvoker is the invoker factory.
func NewInvoker(root, suffix string) *invoker {
	return &invoker{
		root:   path.Clean(root),
		suffix: suffix,
	}
}

// fileName returns the file name that corresponds to the object name.
func (i *invoker) fileName() (string, error) {
	p := path.Join(i.root, i.suffix)
	// Make sure we're not asked to read a file outside of the root
	// directory. This could happen if suffix contains "../", which get
	// collapsed by path.Join().
	if !strings.HasPrefix(p, i.root) {
		return "", errOperationFailed
	}
	return p, nil
}

// Size returns the size of the log file, in bytes.
func (i *invoker) Size(context ipc.ServerContext) (int64, error) {
	vlog.VI(1).Infof("%v.Size()", i.suffix)
	fname, err := i.fileName()
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
func (i *invoker) ReadLog(context ipc.ServerContext, startpos int64, numEntries int32, follow bool, stream logreader.LogFileServiceReadLogStream) (int64, error) {
	vlog.VI(1).Infof("%v.ReadLog(%v, %v, %v)", i.suffix, startpos, numEntries, follow)
	fname, err := i.fileName()
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
	reader := newFollowReader(context, f, startpos, follow)
	if numEntries == logreader.AllEntries {
		numEntries = int32(math.MaxInt32)
	}
	sender := stream.SendStream()
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
		if err := sender.Send(logreader.LogEntry{Position: offset, Line: line}); err != nil {
			return reader.tell(), err
		}
	}
	return reader.tell(), nil
}
