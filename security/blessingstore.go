package security

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"veyron.io/veyron/veyron/security/serialization"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

var errStoreAddMismatch = errors.New("blessing's public key does not match store's public key")

type persistentState struct {
	// Store maps BlessingPatterns to the Blessings object that is to be shared
	// with peers which present blessings of their own that match the pattern.
	//
	// All blessings bind to the same public key.
	Store map[security.BlessingPattern]security.Blessings
	// Default is the default Blessings to be shared with peers for which
	// no other information is available to select blessings.
	Default security.Blessings
}

// blessingStore implements security.BlessingStore.
type blessingStore struct {
	publicKey     security.PublicKey
	persistedData SerializerReaderWriter
	signer        serialization.Signer
	mu            sync.RWMutex
	state         persistentState // GUARDED_BY(mu)
}

func (bs *blessingStore) Set(blessings security.Blessings, forPeers security.BlessingPattern) (security.Blessings, error) {
	if !forPeers.IsValid() {
		return nil, fmt.Errorf("%q is an invalid BlessingPattern", forPeers)
	}
	if blessings != nil && !reflect.DeepEqual(blessings.PublicKey(), bs.publicKey) {
		return nil, errStoreAddMismatch
	}
	bs.mu.Lock()
	defer bs.mu.Unlock()
	old, hadold := bs.state.Store[forPeers]
	if blessings != nil {
		bs.state.Store[forPeers] = blessings
	} else {
		delete(bs.state.Store, forPeers)
	}
	if err := bs.save(); err != nil {
		if hadold {
			bs.state.Store[forPeers] = old
		} else {
			delete(bs.state.Store, forPeers)
		}
		return nil, err
	}
	return old, nil
}

func (bs *blessingStore) ForPeer(peerBlessings ...string) security.Blessings {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	var ret security.Blessings
	for pattern, blessings := range bs.state.Store {
		if pattern.MatchedBy(peerBlessings...) {
			if union, err := security.UnionOfBlessings(ret, blessings); err != nil {
				vlog.Errorf("UnionOfBlessings(%v, %v) failed: %v, dropping the latter from BlessingStore.ForPeers(%v)", ret, blessings, err, peerBlessings)
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
	if bs.state.Default != nil {
		return bs.state.Default
	}
	return bs.ForPeer()
}

func (bs *blessingStore) SetDefault(blessings security.Blessings) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if !reflect.DeepEqual(blessings.PublicKey(), bs.publicKey) {
		return errStoreAddMismatch
	}
	oldDefault := bs.state.Default
	bs.state.Default = blessings
	if err := bs.save(); err != nil {
		bs.state.Default = oldDefault
	}
	return nil
}

func (bs *blessingStore) PublicKey() security.PublicKey {
	return bs.publicKey
}

func (bs *blessingStore) String() string {
	return fmt.Sprintf("{state: %v, publicKey: %v}", bs.state, bs.publicKey)
}

// DebugString return a human-readable string encoding of the store
// in the following format
// Default blessing : <Default blessing of the store>
//
// Peer pattern : Blessings
// <pattern>    : <blessings>
// ...
// <pattern>    : <blessings>
func (bs *blessingStore) DebugString() string {
	const format = "%-30s : %s\n"
	b := bytes.NewBufferString(fmt.Sprintf("Default blessings: %v\n", bs.state.Default))

	b.WriteString(fmt.Sprintf(format, "Peer pattern", "Blessings"))
	for pattern, blessings := range bs.state.Store {
		b.WriteString(fmt.Sprintf(format, pattern, blessings))
	}
	return b.String()
}

func (bs *blessingStore) save() error {
	if (bs.signer == nil) && (bs.persistedData == nil) {
		return nil
	}
	data, signature, err := bs.persistedData.Writers()
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
		state:     persistentState{Store: make(map[security.BlessingPattern]security.Blessings)},
	}
}

// newPersistingBlessingStore returns a security.BlessingStore for a principal
// that is initialized with the persisted data. The returned security.BlessingStore
// also persists any updates to its state.
func newPersistingBlessingStore(persistedData SerializerReaderWriter, signer serialization.Signer) (security.BlessingStore, error) {
	if persistedData == nil || signer == nil {
		return nil, errors.New("persisted data or signer is not specified")
	}
	bs := &blessingStore{
		publicKey:     signer.PublicKey(),
		state:         persistentState{Store: make(map[security.BlessingPattern]security.Blessings)},
		persistedData: persistedData,
		signer:        signer,
	}
	data, signature, err := bs.persistedData.Readers()
	if err != nil {
		return nil, err
	}
	if data != nil && signature != nil {
		if err := decodeFromStorage(&bs.state, data, signature, bs.signer.PublicKey()); err != nil {
			return nil, err
		}
	}
	for _, b := range bs.state.Store {
		if !reflect.DeepEqual(b.PublicKey(), bs.publicKey) {
			return nil, fmt.Errorf("directory contains Blessings: %v that are not for the provided PublicKey: %v", b, bs.publicKey)
		}
	}
	return bs, nil
}
