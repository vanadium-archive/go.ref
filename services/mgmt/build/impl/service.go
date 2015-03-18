package impl

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"v.io/v23/rpc"
	"v.io/v23/services/mgmt/binary"
	"v.io/v23/services/mgmt/build"
	"v.io/v23/verror"
	"v.io/x/lib/envutil"
	"v.io/x/lib/vlog"
)

const pkgPath = "v.io/x/ref/services/mgmt/build/impl"

// Errors
var (
	errBuildFailed = verror.Register(pkgPath+".errBuildFailed", verror.NoRetry, "{1:}{2:} build failed{:_}")
)

// builderService implements the Builder server interface.
type builderService struct {
	// Path to the binary and the value of the GOROOT environment variable.
	gobin, goroot string
}

// NewBuilderService returns a new Build service implementation.
func NewBuilderService(gobin, goroot string) build.BuilderServerMethods {
	return &builderService{
		gobin:  gobin,
		goroot: goroot,
	}
}

// TODO(jsimsa): Add support for building for a specific profile
// specified as a suffix the Build().
//
// TODO(jsimsa): Analyze the binary files for shared library
// dependencies and ship these back.
func (i *builderService) Build(call build.BuilderBuildServerCall, arch build.Architecture, opsys build.OperatingSystem) ([]byte, error) {
	vlog.VI(1).Infof("Build(%v, %v) called.", arch, opsys)
	dir, prefix := "", ""
	dirPerm, filePerm := os.FileMode(0700), os.FileMode(0600)
	root, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		vlog.Errorf("TempDir(%v, %v) failed: %v", dir, prefix, err)
		return nil, verror.New(verror.ErrInternal, call.Context())
	}
	defer os.RemoveAll(root)
	if err := os.Chdir(root); err != nil {
		vlog.Errorf("Chdir(%v) failed: %v", root, err)
	}
	srcDir := filepath.Join(root, "go", "src")
	if err := os.MkdirAll(srcDir, dirPerm); err != nil {
		vlog.Errorf("MkdirAll(%v, %v) failed: %v", srcDir, dirPerm, err)
		return nil, verror.New(verror.ErrInternal, call.Context())
	}
	iterator := call.RecvStream()
	for iterator.Advance() {
		srcFile := iterator.Value()
		filePath := filepath.Join(srcDir, filepath.FromSlash(srcFile.Name))
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			vlog.Errorf("MkdirAll(%v, %v) failed: %v", dir, dirPerm, err)
			return nil, verror.New(verror.ErrInternal, call.Context())
		}
		if err := ioutil.WriteFile(filePath, srcFile.Contents, filePerm); err != nil {
			vlog.Errorf("WriteFile(%v, %v) failed: %v", filePath, filePerm, err)
			return nil, verror.New(verror.ErrInternal, call.Context())
		}
	}
	if err := iterator.Err(); err != nil {
		vlog.Errorf("Advance() failed: %v", err)
		return nil, verror.New(verror.ErrInternal, call.Context())
	}
	cmd := exec.Command(i.gobin, "install", "-v", "./...")
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
		return output.Bytes(), verror.New(errBuildFailed, call.Context())
	}
	binDir := filepath.Join(root, "go", "bin")
	machineArch, err := envutil.Arch()
	if err != nil {
		vlog.Errorf("Arch() failed: %v", err)
		return nil, verror.New(verror.ErrInternal, call.Context())
	}
	if machineArch != string(arch) || runtime.GOOS != string(opsys) {
		binDir = filepath.Join(binDir, fmt.Sprintf("%v_%v", string(opsys), string(arch)))
	}
	files, err := ioutil.ReadDir(binDir)
	if err != nil && !os.IsNotExist(err) {
		vlog.Errorf("ReadDir(%v) failed: %v", binDir, err)
		return nil, verror.New(verror.ErrInternal, call.Context())
	}
	for _, file := range files {
		binPath := filepath.Join(binDir, file.Name())
		bytes, err := ioutil.ReadFile(binPath)
		if err != nil {
			vlog.Errorf("ReadFile(%v) failed: %v", binPath, err)
			return nil, verror.New(verror.ErrInternal, call.Context())
		}
		result := build.File{
			Name:     "bin/" + file.Name(),
			Contents: bytes,
		}
		if err := call.SendStream().Send(result); err != nil {
			vlog.Errorf("Send() failed: %v", err)
			return nil, verror.New(verror.ErrInternal, call.Context())
		}
	}
	return output.Bytes(), nil
}

func (i *builderService) Describe(_ rpc.ServerCall, name string) (binary.Description, error) {
	// TODO(jsimsa): Implement.
	return binary.Description{}, nil
}
