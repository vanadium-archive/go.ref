package impl

import (
	"os"
	"path"

	"veyron/lib/glob"

	"veyron2/ipc"
	"veyron2/services/mounttable/types"
	"veyron2/vlog"
)

// logDirectoryInvoker holds the state of an invocation.
type logDirectoryInvoker struct {
	// root is the root directory from which the object names are based.
	root string
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative object name to identify a log directory.
	suffix string
}

// NewLogDirectoryInvoker is the invoker factory.
func NewLogDirectoryInvoker(root, suffix string) ipc.Invoker {
	return ipc.ReflectInvoker(&logDirectoryInvoker{path.Clean(root), suffix})
}

// Glob streams the name of all the objects that match pattern.
func (i *logDirectoryInvoker) Glob(call ipc.ServerCall, pattern string) error {
	vlog.VI(1).Infof("%v.Glob(%v)", i.suffix, pattern)
	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}
	i.root = path.Join(i.root, i.suffix)
	return i.globStep("", g, true, call)
}

// globStep applies a glob recursively.
func (i *logDirectoryInvoker) globStep(name string, g *glob.Glob, isDir bool, call ipc.ServerCall) error {
	if g.Len() == 0 && !isDir {
		if err := call.Send(types.MountEntry{Name: name}); err != nil {
			return err
		}
	}
	if g.Finished() || !isDir {
		return nil
	}
	dirName, err := translateNameToFilename(i.root, name)
	if err != nil {
		return err
	}
	f, err := os.Open(dirName)
	if err != nil {
		if os.IsNotExist(err) {
			return errNotFound
		}
		return errOperationFailed
	}
	fi, err := f.Readdir(0)
	if err != nil {
		return errOperationFailed
	}
	f.Close()
	for _, file := range fi {
		fileName := file.Name()
		if fileName == "." || fileName == ".." {
			continue
		}
		if ok, left := g.MatchInitialSegment(fileName); ok {
			if err := i.globStep(path.Join(name, fileName), left, file.IsDir(), call); err != nil {
				return err
			}
		}

	}
	return nil
}
