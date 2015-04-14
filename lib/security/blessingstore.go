// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package security

// TODO(ashankar,ataly): This file is a bit of a mess!! Define a serialization
// format for the blessing store and rewrite this file before release!

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/serialization"
)

var (
	errStoreAddMismatch        = verror.Register(pkgPath+".errStoreAddMismatch", verror.NoRetry, "{1:}{2:} blessing's public key does not match store's public key{:_}")
	errBadBlessingPattern      = verror.Register(pkgPath+".errBadBlessingPattern", verror.NoRetry, "{1:}{2:} {3} is an invalid BlessingPattern{:_}")
	errBlessingsNotForKey      = verror.Register(pkgPath+".errBlessingsNotForKey", verror.NoRetry, "{1:}{2:} read Blessings: {3} that are not for provided PublicKey{:_}")
	errDataOrSignerUnspecified = verror.Register(pkgPath+".errDataOrSignerUnspecified", verror.NoRetry, "{1:}{2:} persisted data or signer is not specified{:_}")
)

// TODO(ataly, ashankar): Get rid of this struct once we have switched all
// credentials directories to the new serialization format.
type blessings struct {
	Value security.Blessings
}

// TODO(ataly, ashankar): Get rid of this struct once we have switched all
// credentials directories to the new serialization format.
type state struct {
	// Store maps BlessingPatterns to the Blessings object that is to be shared
	// with peers which present blessings of their own that match the pattern.
	//
	// All blessings bind to the same public key.
	Store map[security.BlessingPattern]*blessings
	// Default is the default Blessings to be shared with peers for which
	// no other information is available to select blessings.
	Default *blessings
}

// blessingStore implements security.BlessingStore.
type blessingStore struct {
	publicKey  security.PublicKey
	serializer SerializerReaderWriter
	signer     serialization.Signer
	mu         sync.RWMutex
	state      blessingStoreState // GUARDED_BY(mu)
}

func (bs *blessingStore) Set(blessings security.Blessings, forPeers security.BlessingPattern) (security.Blessings, error) {
	if !forPeers.IsValid() {
		return security.Blessings{}, verror.New(errBadBlessingPattern, nil, forPeers)
	}
	if !blessings.IsZero() && !reflect.DeepEqual(blessings.PublicKey(), bs.publicKey) {
		return security.Blessings{}, verror.New(errStoreAddMismatch, nil)
	}
	bs.mu.Lock()
	defer bs.mu.Unlock()
	old, hadold := bs.state.PeerBlessings[forPeers]
	if !blessings.IsZero() {
		bs.state.PeerBlessings[forPeers] = blessings
	} else {
		delete(bs.state.PeerBlessings, forPeers)
	}
	if err := bs.save(); err != nil {
		if hadold {
			bs.state.PeerBlessings[forPeers] = old
		} else {
			delete(bs.state.PeerBlessings, forPeers)
		}
		return security.Blessings{}, err
	}
	return old, nil
}

func (bs *blessingStore) ForPeer(peerBlessings ...string) security.Blessings {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	var ret security.Blessings
	for pattern, b := range bs.state.PeerBlessings {
		if pattern.MatchedBy(peerBlessings...) {
			if union, err := security.UnionOfBlessings(ret, b); err != nil {
				vlog.Errorf("UnionOfBlessings(%v, %v) failed: %v, dropping the latter from BlessingStore.ForPeers(%v)", ret, b, err, peerBlessings)
			} else {
				ret = union
			}
		}
	}
	return ret
}

func (bs *blessingStore) Default() security.Blessings {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.state.DefaultBlessings
}

func (bs *blessingStore) SetDefault(blessings security.Blessings) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if !blessings.IsZero() && !reflect.DeepEqual(blessings.PublicKey(), bs.publicKey) {
		return verror.New(errStoreAddMismatch, nil)
	}
	oldDefault := bs.state.DefaultBlessings
	bs.state.DefaultBlessings = blessings
	if err := bs.save(); err != nil {
		bs.state.DefaultBlessings = oldDefault
		return err
	}
	return nil
}

func (bs *blessingStore) PublicKey() security.PublicKey {
	return bs.publicKey
}

func (bs *blessingStore) String() string {
	return fmt.Sprintf("{state: %v, publicKey: %v}", bs.state, bs.publicKey)
}

func (bs *blessingStore) PeerBlessings() map[security.BlessingPattern]security.Blessings {
	m := make(map[security.BlessingPattern]security.Blessings)
	for pattern, b := range bs.state.PeerBlessings {
		m[pattern] = b
	}
	return m
}

// DebugString return a human-readable string encoding of the store
// in the following format
// Default Blessings <blessings>
// Peer pattern   Blessings
// <pattern>      <blessings>
// ...
// <pattern>      <blessings>
func (bs *blessingStore) DebugString() string {
	const format = "%-30s   %s\n"
	buff := bytes.NewBufferString(fmt.Sprintf(format, "Default Blessings", bs.state.DefaultBlessings))

	buff.WriteString(fmt.Sprintf(format, "Peer pattern", "Blessings"))

	sorted := make([]string, 0, len(bs.state.PeerBlessings))
	for k, _ := range bs.state.PeerBlessings {
		sorted = append(sorted, string(k))
	}
	sort.Strings(sorted)
	for _, pattern := range sorted {
		buff.WriteString(fmt.Sprintf(format, pattern, bs.state.PeerBlessings[security.BlessingPattern(pattern)]))
	}
	return buff.String()
}

func (bs *blessingStore) save() error {
	if (bs.signer == nil) && (bs.serializer == nil) {
		return nil
	}
	data, signature, err := bs.serializer.Writers()
	if err != nil {
		return err
	}
	return encodeAndStore(bs.state, data, signature, bs.signer)
}

// newInMemoryBlessingStore returns an in-memory security.BlessingStore for a
// principal with the provided PublicKey.
//
// The returned BlessingStore is initialized with an empty set of blessings.
func newInMemoryBlessingStore(publicKey security.PublicKey) security.BlessingStore {
	return &blessingStore{
		publicKey: publicKey,
		state:     blessingStoreState{PeerBlessings: make(map[security.BlessingPattern]security.Blessings)},
	}
}

func (bs *blessingStore) verifyState() error {
	for _, b := range bs.state.PeerBlessings {
		if !reflect.DeepEqual(b.PublicKey(), bs.publicKey) {
			return verror.New(errBlessingsNotForKey, nil, b, bs.publicKey)
		}
	}
	if !bs.state.DefaultBlessings.IsZero() && !reflect.DeepEqual(bs.state.DefaultBlessings.PublicKey(), bs.publicKey) {
		return verror.New(errBlessingsNotForKey, nil, bs.state.DefaultBlessings, bs.publicKey)
	}
	return nil
}

// TODO(ataly, ashankar): Get rid of this method once we have switched all
// credentials directories to the new serialization format.
func (bs *blessingStore) deserializeOld() error {
	data, signature, err := bs.serializer.Readers()
	if err != nil {
		return err
	}
	if data == nil && signature == nil {
		return nil
	}

	var old state
	if err := decodeFromStorage(&old, data, signature, bs.signer.PublicKey()); err != nil {
		return err
	}

	for p, wb := range old.Store {
		if wb != nil {
			bs.state.PeerBlessings[p] = wb.Value
		}
	}
	if old.Default != nil {
		bs.state.DefaultBlessings = old.Default.Value
	}

	if err := bs.verifyState(); err != nil {
		return err
	}

	// Save the blessingstore in the new serialization format. This will ensure
	// that all credentials directories in the old format will switch to the new
	// format.
	if err := bs.save(); err != nil {
		return err
	}

	return nil
}

func (bs *blessingStore) deserialize() error {
	data, signature, err := bs.serializer.Readers()
	if err != nil {
		return err
	}
	if data == nil && signature == nil {
		return nil
	}
	if err := decodeFromStorage(&bs.state, data, signature, bs.signer.PublicKey()); err != nil {
		return bs.deserializeOld()
	}
	return bs.verifyState()
}

// newPersistingBlessingStore returns a security.BlessingStore for a principal
// that is initialized with the persisted data. The returned security.BlessingStore
// also persists any updates to its state.
func newPersistingBlessingStore(serializer SerializerReaderWriter, signer serialization.Signer) (security.BlessingStore, error) {
	if serializer == nil || signer == nil {
		return nil, verror.New(errDataOrSignerUnspecified, nil)
	}
	bs := &blessingStore{
		publicKey:  signer.PublicKey(),
		state:      blessingStoreState{PeerBlessings: make(map[security.BlessingPattern]security.Blessings)},
		serializer: serializer,
		signer:     signer,
	}
	if err := bs.deserialize(); err != nil {
		return nil, err
	}
	return bs, nil
}
