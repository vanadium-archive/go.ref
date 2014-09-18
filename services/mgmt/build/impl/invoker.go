package impl

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/services/mgmt/binary"
	"veyron.io/veyron/veyron2/services/mgmt/build"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

var (
	errBuildFailed   = verror.Internalf("build failed")
	errInternalError = verror.Internalf("internal error")
)

// invoker holds the state of a build server invocation.
type invoker struct {
	// Path to the binary and the value of the GOROOT environment variable.
	gobin, goroot string
}

// NewInvoker is the invoker factory.
func NewInvoker(gobin, goroot string) *invoker {
	return &invoker{
		gobin:  gobin,
		goroot: goroot,
	}
}

// BUILD INTERFACE IMPLEMENTATION

// TODO(jsimsa): Add support for building for a specific profile
// specified as a suffix the Build().
//
// TODO(jsimsa): Analyze the binary files for shared library
// dependencies and ship these back.
func (i *invoker) Build(_ ipc.ServerContext, arch build.Architecture, opsys build.OperatingSystem, stream build.BuilderServiceBuildStream) ([]byte, error) {
	vlog.VI(1).Infof("Build(%v, %v) called.", arch, opsys)
	dir, prefix := "", ""
	dirPerm, filePerm := os.FileMode(0700), os.FileMode(0600)
	root, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		vlog.Errorf("TempDir(%v, %v) failed: %v", dir, prefix, err)
		return nil, errInternalError
	}
	defer os.RemoveAll(root)
	srcDir := filepath.Join(root, "go", "src")
	if err := os.MkdirAll(srcDir, dirPerm); err != nil {
		vlog.Errorf("MkdirAll(%v, %v) failed: %v", srcDir, dirPerm, err)
		return nil, errInternalError
	}
	iterator := stream.RecvStream()
	for iterator.Advance() {
		srcFile := iterator.Value()
		filePath := filepath.Join(srcDir, filepath.FromSlash(srcFile.Name))
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			vlog.Errorf("MkdirAll(%v, %v) failed: %v", dir, dirPerm, err)
			return nil, errInternalError
		}
		if err := ioutil.WriteFile(filePath, srcFile.Contents, filePerm); err != nil {
			vlog.Errorf("WriteFile(%v, %v) failed: %v", filePath, filePerm, err)
			return nil, errInternalError
		}
	}
	if err := iterator.Err(); err != nil {
		vlog.Errorf("Advance() failed: %v", err)
		return nil, errInternalError
	}
	cmd := exec.Command(i.gobin, "install", "-v", "...")
	cmd.Env = append(cmd.Env, "GOARCH="+string(arch))
	cmd.Env = append(cmd.Env, "GOOS="+string(opsys))
	cmd.Env = append(cmd.Env, "GOPATH="+filepath.Dir(srcDir))
	if i.goroot != "" {
		cmd.Env = append(cmd.Env, "GOROOT="+i.goroot)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		vlog.Errorf("Run() failed: %v", err)
		if output.Len() != 0 {
			vlog.Errorf("%v", output.String())
		}
		return output.Bytes(), errBuildFailed
	}
	binDir := filepath.Join(root, "go", "bin")
	if runtime.GOARCH != string(arch) || runtime.GOOS != string(opsys) {
		binDir = filepath.Join(binDir, fmt.Sprintf("%v_%v", string(opsys), string(arch)))
	}
	files, err := ioutil.ReadDir(binDir)
	if err != nil && !os.IsNotExist(err) {
		vlog.Errorf("ReadDir(%v) failed: %v", binDir, err)
		return nil, errInternalError
	}
	for _, file := range files {
		binPath := filepath.Join(binDir, file.Name())
		bytes, err := ioutil.ReadFile(binPath)
		if err != nil {
			vlog.Errorf("ReadFile(%v) failed: %v", binPath, err)
			return nil, errInternalError
		}
		result := build.File{
			Name:     "bin/" + file.Name(),
			Contents: bytes,
		}
		if err := stream.SendStream().Send(result); err != nil {
			vlog.Errorf("Send() failed: %v", err)
			return nil, errInternalError
		}
	}
	return output.Bytes(), nil
}

func (i *invoker) Describe(_ ipc.ServerContext, name string) (binary.Description, error) {
	// TODO(jsimsa): Implement.
	return binary.Description{}, nil
}
