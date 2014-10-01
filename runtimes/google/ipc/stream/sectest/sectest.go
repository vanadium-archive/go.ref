// Package sectest provides test utility functions for security-related operations for tests within veyron.io/veyron/veyron/runtimes/google/ipc/stream.
//
// TODO(ashankar,ataly): Figure out what to do with the BlessingStore implementation and move it to
// veyron.io/veyron/veyron2/security/sectest. In the mean time this package provides just enough hacky
// functionality to work for unittests in veyron.io/veyron/veyron/runtimes/google/ipc/....
package sectest

import (
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/security/sectest"
)

// NewPrincipal creates a new security.Principal which provides
// defaultBlessing in BlessingStore().Default().
func NewPrincipal(defaultBlessing string) security.Principal {
	_, key, err := sectest.NewKey()
	if err != nil {
		panic(err)
	}
	signer := security.NewInMemoryECDSASigner(key)
	store := &blessingStore{m: make(map[string]security.Blessings), k: signer.PublicKey()}
	p, err := security.CreatePrincipal(signer, store, sectest.NewBlessingRoots())
	if err != nil {
		panic(err)
	}
	def, err := p.BlessSelf(defaultBlessing)
	if err != nil {
		panic(err)
	}
	p.BlessingStore().SetDefault(def)
	p.AddToRoots(def)
	return p
}

// security.BlessingStore implementation that holds one default and can mark other
// blessings to be shared with a specific peer.
//
// TODO(ashankar,ataly): Remove this and use a reference implementation from veyron/runtimes/google/rt, which
// should move to veyron/security?
type blessingStore struct {
	d security.Blessings
	m map[string]security.Blessings
	k security.PublicKey
}

func (bs *blessingStore) Add(blessings security.Blessings, peer security.BlessingPattern) error {
	bs.m[string(peer)] = blessings
	return nil
}

func (bs *blessingStore) ForPeer(peers ...string) security.Blessings {
	var ret []security.Blessings
	for _, p := range peers {
		if b := bs.m[p]; b != nil {
			ret = append(ret, b)
		}
	}
	if len(ret) > 0 {
		b, err := security.UnionOfBlessings(ret...)
		if err != nil {
			panic(err)
		}
		return b
	}
	// TODO(ashankar,ataly): This violates the contract in the BlessingStore API comments.
	return bs.d
}

func (bs *blessingStore) SetDefault(b security.Blessings) error {
	bs.d = b
	return nil
}

func (bs *blessingStore) Default() security.Blessings   { return bs.d }
func (bs *blessingStore) PublicKey() security.PublicKey { return bs.k }
