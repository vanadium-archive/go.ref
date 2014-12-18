// Package impl implements the LogFile interface from
// veyron2/services/mgmt/logreader, which can be used to allow remote access to
// log files, and the Globbable interface from veyron2/services/mounttable to
// find the files in a logs directory.
package impl

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/services/mgmt/logreader"
	"veyron.io/veyron/veyron2/services/mgmt/logreader/types"
	verror "veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"
)

const pkgPath = "veyron.io/veyron/veyron/services/mgmt/logreader/impl"

var (
	errEOF             = verror.Register(types.EOF, verror.NoRetry, "{1:}{2:} EOF{:_}")
	errOperationFailed = verror.Register(pkgPath+".errOperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
)

// NewLogFileService returns a new log file server.
func NewLogFileService(root, suffix string) interface{} {
	return logreader.LogFileServer(&logfileService{filepath.Clean(root), suffix})
}

// translateNameToFilename returns the file name that corresponds to the object
// name.
func translateNameToFilename(root, name string) (string, error) {
	name = filepath.Join(strings.Split(name, "/")...)
	p := filepath.Join(root, name)
	// Make sure we're not asked to read a file outside of the root
	// directory. This could happen if suffix contains "../", which get
	// collapsed by filepath.Join().
	if !strings.HasPrefix(p, root) {
		return "", verror.Make(errOperationFailed, nil, name)
	}
	return p, nil
}

// logfileService holds the state of a logfile invocation.
type logfileService struct {
	// root is the root directory from which the object names are based.
	root string
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative object name to identify a log file.
	suffix string
}

// Size returns the size of the log file, in bytes.
func (i *logfileService) Size(ctx ipc.ServerContext) (int64, error) {
	vlog.VI(1).Infof("%v.Size()", i.suffix)
	fname, err := translateNameToFilename(i.root, i.suffix)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(fname)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, verror.Make(verror.NoExist, ctx, fname)
		}
		vlog.Errorf("Stat(%v) failed: %v", fname, err)
		return 0, verror.Make(errOperationFailed, ctx, fname)
	}
	if fi.IsDir() {
		return 0, verror.Make(errOperationFailed, ctx, fname)
	}
	return fi.Size(), nil
}

// ReadLog returns log entries from the log file.
func (i *logfileService) ReadLog(ctx logreader.LogFileReadLogContext, startpos int64, numEntries int32, follow bool) (int64, error) {
	vlog.VI(1).Infof("%v.ReadLog(%v, %v, %v)", i.suffix, startpos, numEntries, follow)
	fname, err := translateNameToFilename(i.root, i.suffix)
	if err != nil {
		return 0, err
	}
	f, err := os.Open(fname)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, verror.Make(verror.NoExist, ctx, fname)
		}
		return 0, verror.Make(errOperationFailed, ctx, fname)
	}
	reader := newFollowReader(ctx, f, startpos, follow)
	if numEntries == types.AllEntries {
		numEntries = int32(math.MaxInt32)
	}
	for n := int32(0); n < numEntries; n++ {
		line, offset, err := reader.readLine()
		if err == io.EOF && n > 0 {
			return reader.tell(), nil
		}
		if err == io.EOF {
			return reader.tell(), verror.Make(errEOF, ctx)
		}
		if err != nil {
			return reader.tell(), verror.Make(errOperationFailed, ctx, fname)
		}
		if err := ctx.SendStream().Send(types.LogEntry{Position: offset, Line: line}); err != nil {
			return reader.tell(), err
		}
	}
	return reader.tell(), nil
}

// GlobChildren__ returns the list of files in a directory streamed on a
// channel. The list is empty if the object is a file.
func (i *logfileService) GlobChildren__(ctx ipc.ServerContext) (<-chan string, error) {
	vlog.VI(1).Infof("%v.GlobChildren__()", i.suffix)
	dirName, err := translateNameToFilename(i.root, i.suffix)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(dirName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, verror.Make(verror.NoExist, ctx, dirName)
		}
		return nil, verror.Make(errOperationFailed, ctx, dirName)
	}
	if !stat.IsDir() {
		return nil, nil
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		f, err := os.Open(dirName)
		if err != nil {
			return
		}
		defer f.Close()
		for {
			fi, err := f.Readdir(100)
			if err != nil {
				return
			}
			for _, file := range fi {
				ch <- file.Name()
			}
		}
	}()
	return ch, nil
}
