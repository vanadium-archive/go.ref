// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl_test

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/application"
	"v.io/v23/services/binary"
	"v.io/v23/services/repository"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	"v.io/x/ref/services/internal/servicetest"
)

const mockBinaryRepoName = "br"
const mockApplicationRepoName = "ar"

func startMockRepos(t *testing.T, ctx *context.T) (*application.Envelope, func()) {
	envelope, appCleanup := startApplicationRepository(ctx)
	binaryCleanup := startBinaryRepository(ctx)

	return envelope, func() {
		binaryCleanup()
		appCleanup()
	}
}

// startApplicationRepository sets up a server running the application
// repository.  It returns a pointer to the envelope that the repository returns
// to clients (so that it can be changed).  It also returns a cleanup function.
func startApplicationRepository(ctx *context.T) (*application.Envelope, func()) {
	server, _ := servicetest.NewServer(ctx)
	invoker := new(arInvoker)
	name := mockApplicationRepoName
	if err := server.Serve(name, repository.ApplicationServer(invoker), &openAuthorizer{}); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", name, err)
	}
	return &invoker.envelope, func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

type openAuthorizer struct{}

func (openAuthorizer) Authorize(*context.T, security.Call) error { return nil }

// arInvoker holds the state of an application repository invocation mock.  The
// mock returns the value of the wrapped envelope, which can be subsequently be
// changed at any time.  Client is responsible for synchronization if desired.
type arInvoker struct {
	envelope application.Envelope
}

// APPLICATION REPOSITORY INTERFACE IMPLEMENTATION
func (i *arInvoker) Match(_ *context.T, _ rpc.ServerCall, profiles []string) (application.Envelope, error) {
	vlog.VI(1).Infof("Match()")
	if want := []string{"test-profile"}; !reflect.DeepEqual(profiles, want) {
		return application.Envelope{}, fmt.Errorf("Expected profiles %v, got %v", want, profiles)
	}
	return i.envelope, nil
}

func (i *arInvoker) GetPermissions(*context.T, rpc.ServerCall) (acl access.Permissions, version string, err error) {
	return nil, "", nil
}

func (i *arInvoker) SetPermissions(_ *context.T, _ rpc.ServerCall, acl access.Permissions, version string) error {
	return nil
}

// brInvoker holds the state of a binary repository invocation mock.  It always
// serves the current running binary.
type brInvoker struct{}

// startBinaryRepository sets up a server running the binary repository and
// returns a cleanup function.
func startBinaryRepository(ctx *context.T) func() {
	server, _ := servicetest.NewServer(ctx)
	name := mockBinaryRepoName
	if err := server.Serve(name, repository.BinaryServer(new(brInvoker)), &openAuthorizer{}); err != nil {
		vlog.Fatalf("Serve(%q) failed: %v", name, err)
	}
	return func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

// BINARY REPOSITORY INTERFACE IMPLEMENTATION

// TODO(toddw): Move the errors from dispatcher.go into a common location.
const pkgPath = "v.io/x/ref/services/device/internal/impl"

var ErrOperationFailed = verror.Register(pkgPath+".OperationFailed", verror.NoRetry, "")

func (*brInvoker) Create(*context.T, rpc.ServerCall, int32, repository.MediaInfo) error {
	vlog.VI(1).Infof("Create()")
	return nil
}

func (i *brInvoker) Delete(*context.T, rpc.ServerCall) error {
	vlog.VI(1).Infof("Delete()")
	return nil
}

func (i *brInvoker) Download(ctx *context.T, call repository.BinaryDownloadServerCall, _ int32) error {
	vlog.VI(1).Infof("Download()")
	file, err := os.Open(os.Args[0])
	if err != nil {
		vlog.Errorf("Open() failed: %v", err)
		return verror.New(ErrOperationFailed, ctx)
	}
	defer file.Close()
	bufferLength := 4096
	buffer := make([]byte, bufferLength)
	sender := call.SendStream()
	for {
		n, err := file.Read(buffer)
		switch err {
		case io.EOF:
			return nil
		case nil:
			if err := sender.Send(buffer[:n]); err != nil {
				vlog.Errorf("Send() failed: %v", err)
				return verror.New(ErrOperationFailed, ctx)
			}
		default:
			vlog.Errorf("Read() failed: %v", err)
			return verror.New(ErrOperationFailed, ctx)
		}
	}
}

func (*brInvoker) DownloadUrl(*context.T, rpc.ServerCall) (string, int64, error) {
	vlog.VI(1).Infof("DownloadUrl()")
	return "", 0, nil
}

func (*brInvoker) Stat(ctx *context.T, call rpc.ServerCall) ([]binary.PartInfo, repository.MediaInfo, error) {
	vlog.VI(1).Infof("Stat()")
	h := md5.New()
	bytes, err := ioutil.ReadFile(os.Args[0])
	if err != nil {
		return []binary.PartInfo{}, repository.MediaInfo{}, verror.New(ErrOperationFailed, ctx)
	}
	h.Write(bytes)
	part := binary.PartInfo{Checksum: hex.EncodeToString(h.Sum(nil)), Size: int64(len(bytes))}
	return []binary.PartInfo{part}, repository.MediaInfo{Type: "application/octet-stream"}, nil
}

func (i *brInvoker) Upload(*context.T, repository.BinaryUploadServerCall, int32) error {
	vlog.VI(1).Infof("Upload()")
	return nil
}

func (i *brInvoker) GetPermissions(*context.T, rpc.ServerCall) (acl access.Permissions, version string, err error) {
	return nil, "", nil
}

func (i *brInvoker) SetPermissions(_ *context.T, _ rpc.ServerCall, acl access.Permissions, version string) error {
	return nil
}
