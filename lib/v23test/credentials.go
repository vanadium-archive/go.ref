// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v23test

import (
	"io/ioutil"
	"path/filepath"

	"v.io/v23/security"
	libsec "v.io/x/ref/lib/security"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/agentlib"
	"v.io/x/ref/services/agent/keymgr"
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
	rootDir string
}

func newFilesystemPrincipalManager(rootDir string) principalManager {
	return &filesystemPrincipalManager{rootDir: rootDir}
}

func (m *filesystemPrincipalManager) New() (string, error) {
	dir, err := ioutil.TempDir(m.rootDir, "")
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

type agentPrincipalManager struct {
	rootDir string
	agent   agent.KeyManager
}

func newAgentPrincipalManager(rootDir string) (principalManager, error) {
	agent, err := keymgr.NewLocalAgent(rootDir, nil)
	if err != nil {
		return nil, err
	}
	return &agentPrincipalManager{rootDir: rootDir, agent: agent}, nil
}

func (m *agentPrincipalManager) New() (string, error) {
	id, err := m.agent.NewPrincipal(true)
	if err != nil {
		return "", err
	}
	dir, err := ioutil.TempDir(m.rootDir, "")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "sock")
	if err := m.agent.ServePrincipal(id, path); err != nil {
		return "", err
	}
	return path, nil
}

func (m *agentPrincipalManager) Principal(handle string) (security.Principal, error) {
	return agentlib.NewAgentPrincipalX(handle)
}
