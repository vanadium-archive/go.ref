// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package security

import (
	"v.io/v23/security"
	"v.io/v23/verror"
)

var errImmutablePrincipal = verror.Register(pkgPath+".errImmutablePrincipal", verror.NoRetry, "Principal is immutable")

// NewImmutablePrincipal returns an immutable version of the given Principal.
// All the methods that would normally change the state of the Principal return
// an error instead.
func NewImmutablePrincipal(p security.Principal) security.Principal {
	return &immutablePrincipal{
		p, &immutableBlessingStore{p.BlessingStore()}, &immutableBlessingRoots{p.Roots()},
	}
}

type immutablePrincipal struct {
	security.Principal
	store *immutableBlessingStore
	roots *immutableBlessingRoots
}

func (p *immutablePrincipal) BlessingStore() security.BlessingStore {
	return p.store
}

func (p *immutablePrincipal) Roots() security.BlessingRoots {
	return p.roots
}

func (p *immutablePrincipal) AddToRoots(security.Blessings) error {
	return verror.New(errImmutablePrincipal, nil)
}

type immutableBlessingStore struct {
	security.BlessingStore
}

func (s *immutableBlessingStore) Set(security.Blessings, security.BlessingPattern) (security.Blessings, error) {
	return security.Blessings{}, verror.New(errImmutablePrincipal, nil)
}

func (s *immutableBlessingStore) SetDefault(security.Blessings) error {
	return verror.New(errImmutablePrincipal, nil)
}

type immutableBlessingRoots struct {
	security.BlessingRoots
}

func (r *immutableBlessingRoots) Add([]byte, security.BlessingPattern) error {
	return verror.New(errImmutablePrincipal, nil)
}
