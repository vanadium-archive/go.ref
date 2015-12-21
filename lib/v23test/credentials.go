// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v23test

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"v.io/v23/security"
	libsec "v.io/x/ref/lib/security"
)

// Credentials represents a principal with a set of blessings. It is designed to
// be implementable using either the filesystem (with a credentials dir) or the
// Vanadium security agent.
type Credentials struct {
	Handle    string
	Principal security.Principal // equal to m.Principal(Handle)
	m         principalManager
}

// newCredentials creates a new Credentials.
func newCredentials(m principalManager) (*Credentials, error) {
	h, err := m.New()
	if err != nil {
		return nil, err
	}
	p, err := m.Principal(h)
	if err != nil {
		return nil, err
	}
	return &Credentials{Handle: h, Principal: p, m: m}, nil
}

// newRootCredentials creates a new Credentials with a self-signed "root"
// blessing.
func newRootCredentials(m principalManager) (*Credentials, error) {
	res, err := newCredentials(m)
	if err != nil {
		return nil, err
	}
	if err := libsec.InitDefaultBlessings(res.Principal, "root"); err != nil {
		return nil, err
	}
	return res, nil
}

func addDefaultBlessing(self, other security.Principal, extension string) error {
	blessings, err := self.Bless(other.PublicKey(), self.BlessingStore().Default(), extension, security.UnconstrainedUse())
	if err != nil {
		return err
	}
	union, err := security.UnionOfBlessings(other.BlessingStore().Default(), blessings)
	if err != nil {
		return err
	}
	return libsec.SetDefaultBlessings(other, union)
}

// Fork creates a new Credentials (with a fresh principal) and blesses it with
// the given extensions and no caveats, using this principal's default
// blessings. In addition, it calls SetDefaultBlessings.
func (c *Credentials) Fork(extensions ...string) (*Credentials, error) {
	res, err := newCredentials(c.m)
	if err != nil {
		return nil, err
	}
	for _, extension := range extensions {
		if err := addDefaultBlessing(c.Principal, res.Principal, extension); err != nil {
			return nil, err
		}
	}
	return res, nil
}

////////////////////////////////////////////////////////////////////////////////
// principalManager interface and implementations

// principalManager manages principals.
type principalManager interface {
	// New creates a principal and returns a handle to it.
	New() (string, error)

	// Principal returns the principal for the given handle.
	Principal(handle string) (security.Principal, error)
}

////////////////////////////////////////
// filesystemPrincipalManager

type filesystemPrincipalManager struct {
	rootDir string // directory in which to make per-principal dirs
}

func newFilesystemPrincipalManager(rootDir string) principalManager {
	return &filesystemPrincipalManager{rootDir: rootDir}
}

func (m *filesystemPrincipalManager) New() (string, error) {
	dir, err := ioutil.TempDir(m.rootDir, filepath.Base(os.Args[0])+"."+time.Now().UTC().Format("20060102.150405.000"))
	if err != nil {
		return "", err
	}
	if _, err := libsec.CreatePersistentPrincipal(dir, nil); err != nil {
		return "", err
	}
	return dir, nil
}

func (m *filesystemPrincipalManager) Principal(handle string) (security.Principal, error) {
	return libsec.LoadPersistentPrincipal(handle, nil)
}

////////////////////////////////////////
// agentPrincipalManager

// TODO(sadovsky): Implement.

var errNotImplemented = errors.New("not implemented")

type agentPrincipalManager struct {
	path string // path to the agent's socket
}

func newAgentPrincipalManager(path string) principalManager {
	return &agentPrincipalManager{path: path}
}

func (m *agentPrincipalManager) New() (string, error) {
	return "", errNotImplemented
}

func (m *agentPrincipalManager) Principal(handle string) (security.Principal, error) {
	return nil, errNotImplemented
}
