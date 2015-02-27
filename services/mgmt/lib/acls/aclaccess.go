// Package acls provides a library to assist servers implementing
// GetACL/SetACL functions and authorizers where there are
// path-specific ACLs stored individually in files.
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

	"v.io/core/veyron/security/serialization"
)

const (
	pkgPath = "v.io/core/veyron/services/mgmt/lib/acls"
	sigName = "signature"
	aclName = "data"
)

var (
	ErrOperationFailed = verror.Register(pkgPath+".OperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
)

// Locks provides a mutex lock for each acl file path.
type Locks struct {
	pthlks map[string]*sync.Mutex
	lk     sync.Mutex
}

// NewLocks creates a new instance of the lock map.
func NewLocks() *Locks {
	return &Locks{pthlks: make(map[string]*sync.Mutex)}
}

// GetPathACL returns the TaggedACLMap from the data file in dir.
func (locks Locks) GetPathACL(principal security.Principal, dir string) (access.TaggedACLMap, string, error) {
	aclpath := filepath.Join(dir, aclName)
	sigpath := filepath.Join(dir, sigName)
	defer locks.lockPath(dir)()
	return getCore(principal, aclpath, sigpath)
}

// TODO(rjkroege): Improve lock handling.
func (locks Locks) lockPath(dir string) func() {
	locks.lk.Lock()
	lck, contains := locks.pthlks[dir]
	if !contains {
		lck = new(sync.Mutex)
		locks.pthlks[dir] = lck
	}
	locks.lk.Unlock()
	lck.Lock()
	return lck.Unlock
}

func getCore(principal security.Principal, aclpath, sigpath string) (access.TaggedACLMap, string, error) {
	f, err := os.Open(aclpath)
	if err != nil {
		// This path is rarely a fatal error so log informationally only.
		vlog.VI(2).Infof("os.Open(%s) failed: %v", aclpath, err)
		return nil, "", err
	}
	defer f.Close()

	s, err := os.Open(sigpath)
	if err != nil {
		vlog.Errorf("Signatures for ACLs are required: %s unavailable: %v", aclpath, err)
		return nil, "", verror.New(ErrOperationFailed, nil)
	}
	defer s.Close()

	// read and verify the signature of the acl file
	vf, err := serialization.NewVerifyingReader(f, s, principal.PublicKey())
	if err != nil {
		vlog.Errorf("NewVerifyingReader() failed: %v", err)
		return nil, "", verror.New(ErrOperationFailed, nil)
	}

	acl, err := access.ReadTaggedACLMap(vf)
	if err != nil {
		vlog.Errorf("ReadTaggedACLMap(%s) failed: %v", aclpath, err)
		return nil, "", err
	}
	etag, err := ComputeEtag(acl)
	if err != nil {
		vlog.Errorf("acls.ComputeEtag failed: %v", err)
		return nil, "", err
	}
	return acl, etag, nil
}

// SetPathACL writes the specified TaggedACLMap to the provided
// directory with enforcement of etag synchronization mechanism and
// locking.
func (locks Locks) SetPathACL(principal security.Principal, dir string, acl access.TaggedACLMap, etag string) error {
	aclpath := filepath.Join(dir, aclName)
	sigpath := filepath.Join(dir, sigName)
	defer locks.lockPath(dir)()
	_, oetag, err := getCore(principal, aclpath, sigpath)
	if err != nil && !os.IsNotExist(err) {
		return verror.New(ErrOperationFailed, nil)
	}
	if len(etag) > 0 && etag != oetag {
		return verror.NewErrBadEtag(nil)
	}
	return write(principal, aclpath, sigpath, dir, acl)
}

// write writes the specified TaggedACLMap to the aclFile with a
// signature in sigFile.
func write(principal security.Principal, aclFile, sigFile, dir string, acl access.TaggedACLMap) error {
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
		vlog.Errorf("Failed to SaveACL:%v", err)
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
