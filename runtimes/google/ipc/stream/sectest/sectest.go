// Package sectest provides test utility functions for security-related operations for tests within veyron.io/veyron/veyron/runtimes/google/ipc/stream.
//
// TODO(ashankar,ataly): Figure out what to do with BlessingRoots and BlessingStore
// implementations and where this package should live. Should it be in veyron.io/veyron/veyron2/security, OR
// veyron.io/veyron/veyron/security OR should the blessingstore and blessingroots implementations be
// moved to veyron.io/veyron/veyron/runtimes/google/security and those implementations be used? This needs to be
// figured out, but in the mean time this package provides just enough hacky functionality to work for unittests in
// veyron.io/veyron/veyron/runtimes/google/ipc/stream.
package sectest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"

	"veyron.io/veyron/veyron2/security"
)

// NewPrincipal creates a new security.Principal which:
//  (1) Recognizes ALL public keys as valid roots for ALL blessings
//      (which may be fine for unittests)
//  (2) The BlessingStore will provide defaultBlessing in its Default method.
func NewPrincipal(defaultBlessing string) security.Principal {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	signer := security.NewInMemoryECDSASigner(key)
	store := &blessingStore{m: make(map[string]security.Blessings), k: signer.PublicKey()}
	p, err := security.CreatePrincipal(signer, store, blessingRoots{})
	if err != nil {
		panic(err)
	}
	def, err := p.BlessSelf(defaultBlessing)
	if err != nil {
		panic(err)
	}
	p.BlessingStore().SetDefault(def)
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

// security.BlessingRoots implementation that trusts ALL keys.
// Useless implementation generally speaking, but suffices for the tests here!
type blessingRoots struct{}

func (r blessingRoots) Add(security.PublicKey, security.BlessingPattern) error { return nil }
func (r blessingRoots) Recognized(security.PublicKey, string) error            { return nil }
