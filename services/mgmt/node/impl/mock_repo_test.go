package impl_test

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"testing"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/services/mgmt/binary"
	"veyron.io/veyron/veyron2/services/mgmt/repository"
	"veyron.io/veyron/veyron2/vlog"
)

const mockBinaryRepoName = "br"
const mockApplicationRepoName = "ar"

func startMockRepos(t *testing.T) (*application.Envelope, func()) {
	envelope, appCleanup := startApplicationRepository()
	binaryCleanup := startBinaryRepository()

	return envelope, func() {
		binaryCleanup()
		appCleanup()
	}
}

// startApplicationRepository sets up a server running the application
// repository.  It returns a pointer to the envelope that the repository returns
// to clients (so that it can be changed).  It also returns a cleanup function.
func startApplicationRepository() (*application.Envelope, func()) {
	server, _ := newServer()
	invoker := new(arInvoker)
	name := mockApplicationRepoName
	if err := server.Serve(name, repository.NewServerApplication(invoker), nil); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", name, err)
	}
	return &invoker.envelope, func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

// arInvoker holds the state of an application repository invocation mock.  The
// mock returns the value of the wrapped envelope, which can be subsequently be
// changed at any time.  Client is responsible for synchronization if desired.
type arInvoker struct {
	envelope application.Envelope
}

// APPLICATION REPOSITORY INTERFACE IMPLEMENTATION
func (i *arInvoker) Match(ipc.ServerContext, []string) (application.Envelope, error) {
	vlog.VI(1).Infof("Match()")
	return i.envelope, nil
}

// brInvoker holds the state of a binary repository invocation mock.  It always
// serves the current running binary.
type brInvoker struct{}

// startBinaryRepository sets up a server running the binary repository and
// returns a cleanup function.
func startBinaryRepository() func() {
	server, _ := newServer()
	name := mockBinaryRepoName
	if err := server.Serve(name, repository.NewServerBinary(new(brInvoker)), nil); err != nil {
		vlog.Fatalf("Serve(%q) failed: %v", name, err)
	}
	return func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

// BINARY REPOSITORY INTERFACE IMPLEMENTATION

func (*brInvoker) Create(ipc.ServerContext, int32) error {
	vlog.VI(1).Infof("Create()")
	return nil
}

func (i *brInvoker) Delete(ipc.ServerContext) error {
	vlog.VI(1).Infof("Delete()")
	return nil
}

var errOperationFailed = errors.New("operation failed")

func (i *brInvoker) Download(_ ipc.ServerContext, _ int32, stream repository.BinaryServiceDownloadStream) error {
	vlog.VI(1).Infof("Download()")
	file, err := os.Open(os.Args[0])
	if err != nil {
		vlog.Errorf("Open() failed: %v", err)
		return errOperationFailed
	}
	defer file.Close()
	bufferLength := 4096
	buffer := make([]byte, bufferLength)
	sender := stream.SendStream()
	for {
		n, err := file.Read(buffer)
		switch err {
		case io.EOF:
			return nil
		case nil:
			if err := sender.Send(buffer[:n]); err != nil {
				vlog.Errorf("Send() failed: %v", err)
				return errOperationFailed
			}
		default:
			vlog.Errorf("Read() failed: %v", err)
			return errOperationFailed
		}
	}
}

func (*brInvoker) DownloadURL(ipc.ServerContext) (string, int64, error) {
	vlog.VI(1).Infof("DownloadURL()")
	return "", 0, nil
}

func (*brInvoker) Stat(ipc.ServerContext) ([]binary.PartInfo, error) {
	vlog.VI(1).Infof("Stat()")
	h := md5.New()
	bytes, err := ioutil.ReadFile(os.Args[0])
	if err != nil {
		return []binary.PartInfo{}, errOperationFailed
	}
	h.Write(bytes)
	part := binary.PartInfo{Checksum: hex.EncodeToString(h.Sum(nil)), Size: int64(len(bytes))}
	return []binary.PartInfo{part}, nil
}

func (i *brInvoker) Upload(ipc.ServerContext, int32, repository.BinaryServiceUploadStream) error {
	vlog.VI(1).Infof("Upload()")
	return nil
}
