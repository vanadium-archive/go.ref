// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package acls provides a library to assist servers implementing
// GetPermissions/SetPermissions functions and authorizers where there are
// path-specific AccessLists stored individually in files.
// TODO(rjkroege): Add unit tests.
package acls

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	"v.io/x/ref/security/serialization"
)

const (
	pkgPath = "v.io/x/ref/services/mgmt/lib/acls"
	sigName = "signature"
	aclName = "data"
)

var (
	ErrOperationFailed = verror.Register(pkgPath+".OperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
)

// PathStore manages storage of a set of AccessLists in the filesystem where each
// path identifies a specific AccessList in the set. PathStore synchronizes
// access to its member AccessLists.
type PathStore struct {
	// TODO(rjkroege): Garbage collect the locks map.
	pthlks    map[string]*sync.Mutex
	lk        sync.Mutex
	principal security.Principal
}

// NewPathStore creates a new instance of the lock map that uses
// principal to sign stored AccessList files.
func NewPathStore(principal security.Principal) *PathStore {
	return &PathStore{pthlks: make(map[string]*sync.Mutex), principal: principal}
}

// Get returns the Permissions from the data file in dir.
func (store PathStore) Get(dir string) (access.Permissions, string, error) {
	aclpath := filepath.Join(dir, aclName)
	sigpath := filepath.Join(dir, sigName)
	defer store.lockPath(dir)()
	return getCore(store.principal, aclpath, sigpath)
}

// TODO(rjkroege): Improve lock handling.
func (store PathStore) lockPath(dir string) func() {
	store.lk.Lock()
	lck, contains := store.pthlks[dir]
	if !contains {
		lck = new(sync.Mutex)
		store.pthlks[dir] = lck
	}
	store.lk.Unlock()
	lck.Lock()
	return lck.Unlock
}

func getCore(principal security.Principal, aclpath, sigpath string) (access.Permissions, string, error) {
	f, err := os.Open(aclpath)
	if err != nil {
		// This path is rarely a fatal error so log informationally only.
		vlog.VI(2).Infof("os.Open(%s) failed: %v", aclpath, err)
		return nil, "", err
	}
	defer f.Close()

	s, err := os.Open(sigpath)
	if err != nil {
		vlog.Errorf("Signatures for AccessLists are required: %s unavailable: %v", aclpath, err)
		return nil, "", verror.New(ErrOperationFailed, nil)
	}
	defer s.Close()

	// read and verify the signature of the acl file
	vf, err := serialization.NewVerifyingReader(f, s, principal.PublicKey())
	if err != nil {
		vlog.Errorf("NewVerifyingReader() failed: %v (acl=%s, sig=%s)", err, aclpath, sigpath)
		return nil, "", verror.New(ErrOperationFailed, nil)
	}

	acl, err := access.ReadPermissions(vf)
	if err != nil {
		vlog.Errorf("ReadPermissions(%s) failed: %v", aclpath, err)
		return nil, "", err
	}
	etag, err := ComputeEtag(acl)
	if err != nil {
		vlog.Errorf("acls.ComputeEtag failed: %v", err)
		return nil, "", err
	}
	return acl, etag, nil
}

// Set writes the specified Permissions to the provided
// directory with enforcement of etag synchronization mechanism and
// locking.
func (store PathStore) Set(dir string, acl access.Permissions, etag string) error {
	aclpath := filepath.Join(dir, aclName)
	sigpath := filepath.Join(dir, sigName)
	defer store.lockPath(dir)()
	_, oetag, err := getCore(store.principal, aclpath, sigpath)
	if err != nil && !os.IsNotExist(err) {
		return verror.New(ErrOperationFailed, nil)
	}
	if len(etag) > 0 && etag != oetag {
		return verror.NewErrBadEtag(nil)
	}
	return write(store.principal, aclpath, sigpath, dir, acl)
}

// write writes the specified Permissions to the aclFile with a
// signature in sigFile.
func write(principal security.Principal, aclFile, sigFile, dir string, acl access.Permissions) error {
	// Create dir directory if it does not exist
	os.MkdirAll(dir, os.FileMode(0700))
	// Save the object to temporary data and signature files, and then move
	// those files to the actual data and signature file.
	data, err := ioutil.TempFile(dir, aclName)
	if err != nil {
		vlog.Errorf("Failed to open tmpfile data:%v", err)
		return verror.New(ErrOperationFailed, nil)
	}
	defer os.Remove(data.Name())
	sig, err := ioutil.TempFile(dir, sigName)
	if err != nil {
		vlog.Errorf("Failed to open tmpfile sig:%v", err)
		return verror.New(ErrOperationFailed, nil)
	}
	defer os.Remove(sig.Name())
	writer, err := serialization.NewSigningWriteCloser(data, sig, principal, nil)
	if err != nil {
		vlog.Errorf("Failed to create NewSigningWriteCloser:%v", err)
		return verror.New(ErrOperationFailed, nil)
	}
	if err = acl.WriteTo(writer); err != nil {
		vlog.Errorf("Failed to SaveAccessList:%v", err)
		return verror.New(ErrOperationFailed, nil)
	}
	if err = writer.Close(); err != nil {
		vlog.Errorf("Failed to Close() SigningWriteCloser:%v", err)
		return verror.New(ErrOperationFailed, nil)
	}
	if err := os.Rename(data.Name(), aclFile); err != nil {
		vlog.Errorf("os.Rename() failed:%v", err)
		return verror.New(ErrOperationFailed, nil)
	}
	if err := os.Rename(sig.Name(), sigFile); err != nil {
		vlog.Errorf("os.Rename() failed:%v", err)
		return verror.New(ErrOperationFailed, nil)
	}
	return nil
}

func (store PathStore) TAMForPath(path string) (access.Permissions, bool, error) {
	tam, _, err := store.Get(path)
	if os.IsNotExist(err) {
		return nil, true, nil
	} else if err != nil {
		return nil, false, err
	}
	return tam, false, nil
}
