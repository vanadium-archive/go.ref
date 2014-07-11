package impl

import (
	"errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"veyron2/ipc"
	"veyron2/services/mgmt/binary"
	"veyron2/services/mgmt/build"
	"veyron2/vlog"
)

var (
	errOperationFailed = errors.New("operation failed")
)

// invoker holds the state of a build server invocation.
type invoker struct {
	// gobin is the path to the Go compiler binary.
	gobin string
}

// NewInvoker is the invoker factory.
func NewInvoker(gobin string) *invoker {
	return &invoker{
		gobin: gobin,
	}
}

// BUILD INTERFACE IMPLEMENTATION

func (i *invoker) Build(_ ipc.ServerContext, stream build.BuildServiceBuildStream) ([]byte, error) {
	dir, prefix := "", ""
	dirPerm, filePerm := os.FileMode(0700), os.FileMode(0600)
	root, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		vlog.Errorf("TempDir(%v, %v) failed: %v", dir, prefix, err)
		return nil, errOperationFailed
	}
	defer os.RemoveAll(root)
	srcDir := filepath.Join(root, "go", "src")
	if err := os.MkdirAll(srcDir, dirPerm); err != nil {
		vlog.Errorf("MkdirAll(%v, %v) failed: %v", srcDir, dirPerm, err)
		return nil, errOperationFailed
	}
	for {
		srcFile, err := stream.Recv()
		if err != nil && err != io.EOF {
			vlog.Errorf("Recv() failed: %v", err)
			return nil, errOperationFailed
		}
		if err == io.EOF {
			break
		}
		filePath := filepath.Join(srcDir, filepath.FromSlash(srcFile.Name))
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			vlog.Errorf("MkdirAll(%v, %v) failed: %v", dir, dirPerm, err)
			return nil, errOperationFailed
		}
		if err := ioutil.WriteFile(filePath, srcFile.Contents, filePerm); err != nil {
			vlog.Errorf("WriteFile(%v, %v) failed: %v", filePath, filePerm, err)
			return nil, errOperationFailed
		}
	}
	cmd := exec.Command(i.gobin, "build", "-v", "...")
	cmd.Env = append(cmd.Env, "GOPATH="+filepath.Dir(srcDir))
	bytes, err := cmd.CombinedOutput()
	if err != nil {
		vlog.Errorf("CombinedOutput() failed: %v", err)
		return nil, errOperationFailed
	}
	return bytes, nil
}

func (i *invoker) Describe(_ ipc.ServerContext, name string) (binary.Description, error) {
	// TODO(jsimsa): Implement.
	return binary.Description{}, nil
}
