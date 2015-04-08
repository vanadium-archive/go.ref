// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mounttablelib

import (
	"strconv"

	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

// TAMG associates a generation with a Permissions
type TAMG struct {
	tam        access.Permissions
	generation int32
}

func NewTAMG() *TAMG {
	return &TAMG{tam: make(access.Permissions)}
}

// Set sets the AccessLists iff generation matches the current generation.  If the set happens, the generation is advanced.
// If b is nil, this creates a new TAMG.
func (b *TAMG) Set(genstr string, tam access.Permissions) (*TAMG, error) {
	if b == nil {
		b = new(TAMG)
	}
	if len(genstr) > 0 {
		gen, err := strconv.ParseInt(genstr, 10, 32)
		if err != nil {
			return b, verror.NewErrBadVersion(nil)
		}
		if gen >= 0 && int32(gen) != b.generation {
			return b, verror.NewErrBadVersion(nil)
		}
	}
	b.tam = tam
	b.generation++
	// Protect against wrap.
	if b.generation < 0 {
		b.generation = 0
	}
	return b, nil
}

// Get returns the current generation and acls.
func (b *TAMG) Get() (string, access.Permissions) {
	if b == nil {
		return "", nil
	}
	return strconv.FormatInt(int64(b.generation), 10), b.tam
}

// GetPermissionsForTag returns the current acls for the given tag.
func (b *TAMG) GetPermissionsForTag(tag string) (access.AccessList, bool) {
	acl, exists := b.tam[tag]
	return acl, exists
}

// Copy copies the receiver.
func (b *TAMG) Copy() *TAMG {
	nt := new(TAMG)
	nt.tam = b.tam.Copy()
	nt.generation = b.generation
	return nt
}

// Add adds the blessing pattern to the tag in the reciever.
func (b *TAMG) Add(pattern security.BlessingPattern, tag string) {
	b.tam.Add(pattern, tag)
}
