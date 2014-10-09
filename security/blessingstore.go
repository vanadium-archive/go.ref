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

const (
	blessingStoreDataFile = "blessingstore.data"
	blessingStoreSigFile  = "blessingstore.sig"
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
	publicKey security.PublicKey
	dir       string
	signer    serialization.Signer
	mu        sync.RWMutex
	state     persistentState // GUARDED_BY(mu)
}

func (s *blessingStore) Set(blessings security.Blessings, forPeers security.BlessingPattern) (security.Blessings, error) {
	if !forPeers.IsValid() {
		return nil, fmt.Errorf("%q is an invalid BlessingPattern", forPeers)
	}
	if blessings != nil && !reflect.DeepEqual(blessings.PublicKey(), s.publicKey) {
		return nil, errStoreAddMismatch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, hadold := s.state.Store[forPeers]
	if blessings != nil {
		s.state.Store[forPeers] = blessings
	} else {
		delete(s.state.Store, forPeers)
	}
	if err := s.save(); err != nil {
		if hadold {
			s.state.Store[forPeers] = old
		} else {
			delete(s.state.Store, forPeers)
		}
		return nil, err
	}
	return old, nil
}

func (s *blessingStore) ForPeer(peerBlessings ...string) security.Blessings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ret security.Blessings
	for pattern, blessings := range s.state.Store {
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

func (s *blessingStore) Default() security.Blessings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.Default != nil {
		return s.state.Default
	}
	return s.ForPeer()
}

func (s *blessingStore) SetDefault(blessings security.Blessings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !reflect.DeepEqual(blessings.PublicKey(), s.publicKey) {
		return errStoreAddMismatch
	}
	oldDefault := s.state.Default
	s.state.Default = blessings
	if err := s.save(); err != nil {
		s.state.Default = oldDefault
	}
	return nil
}

func (s *blessingStore) PublicKey() security.PublicKey {
	return s.publicKey
}

func (s *blessingStore) String() string {
	return fmt.Sprintf("{state: %v, publicKey: %v, dir: %v}", s.state, s.publicKey, s.dir)
}

// DebugString return a human-readable string encoding of the store
// in the following format
// Default blessing : <Default blessing of the store>
//
// Peer pattern : Blessings
// <pattern>    : <blessings>
// ...
// <pattern>    : <blessings>
func (br *blessingStore) DebugString() string {
	const format = "%-30s : %s\n"
	b := bytes.NewBufferString(fmt.Sprintf("Default blessings: %v\n", br.state.Default))

	b.WriteString(fmt.Sprintf(format, "Peer pattern", "Blessings"))
	for pattern, blessings := range br.state.Store {
		b.WriteString(fmt.Sprintf(format, pattern, blessings))
	}
	return b.String()
}

func (s *blessingStore) save() error {
	if (s.signer == nil) && (s.dir == "") {
		return nil
	}
	return encodeAndStore(s.state, s.dir, blessingStoreDataFile, blessingStoreSigFile, s.signer)
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
// that persists all updates to the specified directory and uses the provided
// signer to ensure integrity of data read from the filesystem.
//
// The returned BlessingStore is initialized from the existing data present in
// the directory. The data is verified to have been written by a persisting
// BlessingStore object constructed from the same signer.
//
// Any errors obtained in reading or verifying the data are returned.
func newPersistingBlessingStore(directory string, signer serialization.Signer) (security.BlessingStore, error) {
	if directory == "" || signer == nil {
		return nil, errors.New("directory or signer is not specified")
	}
	s := &blessingStore{
		publicKey: signer.PublicKey(),
		state:     persistentState{Store: make(map[security.BlessingPattern]security.Blessings)},
		dir:       directory,
		signer:    signer,
	}

	if err := decodeFromStorage(&s.state, s.dir, blessingStoreDataFile, blessingStoreSigFile, s.signer.PublicKey()); err != nil {
		return nil, err
	}

	for _, b := range s.state.Store {
		if !reflect.DeepEqual(b.PublicKey(), s.publicKey) {
			return nil, fmt.Errorf("directory contains Blessings: %v that are not for the provided PublicKey: %v", b, s.publicKey)
		}
	}
	return s, nil
}
