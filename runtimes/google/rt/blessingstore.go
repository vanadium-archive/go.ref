package rt

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	isecurity "veyron/runtimes/google/security"
	"veyron/security/serialization"

	"veyron2/security"
	"veyron2/vlog"
)

const (
	blessingStoreDataFile = "blessingstore.data"
	blessingStoreSigFile  = "blessingstore.sig"
)

var errStoreAddMismatch = errors.New("blessing's public key does not match store's public key")

type markedBlessings struct {
	Blessings security.PublicID
	Patterns  []security.BlessingPattern
}

type persistentState struct {
	// Store contains a set of Blessings marked with a set of BlessingsPatterns.
	// These blessings are intended to be shared with peers whose blessings
	// match the corresponding pattern. All Blessings in the store must have
	// the same public key.
	Store []markedBlessings
	// Default is the default Blessings to be shared with peers for which
	// no other information is available to select blessings.
	Default security.PublicID
}

// blessingStore implements security.BlessingStore.
type blessingStore struct {
	state     persistentState // GUARDED_BY(mu)
	publicKey security.PublicKey
	dir       string
	signer    serialization.Signer
	mu        sync.RWMutex
}

func (s *blessingStore) Add(blessings security.PublicID, forPeers security.BlessingPattern) error {
	if !reflect.DeepEqual(blessings.PublicKey(), s.publicKey) {
		return errStoreAddMismatch
	}
	if !forPeers.IsValid() {
		return fmt.Errorf("%q is an invalid BlessingPattern", forPeers)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var entry *markedBlessings
	for i, mb := range s.state.Store {
		if !reflect.DeepEqual(mb.Blessings, blessings) {
			continue
		}
		entry = &s.state.Store[i]
		for _, p := range mb.Patterns {
			if p == forPeers {
				return nil
			}
		}
		break
	}
	if entry == nil {
		s.state.Store = append(s.state.Store, markedBlessings{blessings, []security.BlessingPattern{forPeers}})
	} else {
		entry.Patterns = append(entry.Patterns, forPeers)
	}

	if err := s.save(); err != nil {
		if entry == nil {
			s.state.Store = s.state.Store[:len(s.state.Store)-1]
		} else {
			entry.Patterns = entry.Patterns[:len(entry.Patterns)-1]
		}
		return err
	}
	return nil
}

func (s *blessingStore) ForPeer(peerBlessings ...string) security.PublicID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matchingBlessings []security.PublicID
	for _, mb := range s.state.Store {
		for _, p := range mb.Patterns {
			if p.MatchedBy(peerBlessings...) {
				matchingBlessings = append(matchingBlessings, mb.Blessings)
				break
			}
		}
	}

	blessings, err := isecurity.NewSetPublicID(matchingBlessings...)
	if err != nil {
		// This case should never be hit.
		vlog.Errorf("BlessingStore: %s is broken, could not combine PublicIDs from it: %s", s, err)
		return nil
	}
	return blessings
}

func (s *blessingStore) Default() security.PublicID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.Default != nil {
		return s.state.Default
	}
	return s.ForPeer()
}

func (s *blessingStore) SetDefault(blessings security.PublicID) error {
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

func (s *blessingStore) save() error {
	if (s.signer == nil) && (s.dir == "") {
		return nil
	}
	return encodeAndStore(s.state, s.dir, blessingStoreDataFile, blessingStoreSigFile, s.signer)
}

// NewInMemoryBlessingStore returns an in-memory security.BlessingStore for a
// principal with the provided PublicKey.
//
// The returned BlessingStore is initialized with an empty set of blessings.
func NewInMemoryBlessingStore(publicKey security.PublicKey) security.BlessingStore {
	return &blessingStore{
		publicKey: publicKey,
	}
}

// NewPersistingBlessingStore returns a security.BlessingStore for a principal
// with the provided PublicKey that signs and persists all updates to the
// specified directory. Signing is carried out using the provided signer.
//
// The returned BlessingStore is initialized from the existing data present in
// the directory. The data is verified to have been written by a persisting
// BlessingStore object constructed from the same PublicKey and signer.
//
// Any errors obtained in reading or verifying the data are returned.
func NewPersistingBlessingStore(publicKey security.PublicKey, directory string, signer serialization.Signer) (security.BlessingStore, error) {
	if directory == "" || signer == nil {
		return nil, errors.New("directory or signer is not specified")
	}
	s := &blessingStore{
		publicKey: publicKey,
		dir:       directory,
		signer:    signer,
	}

	if err := decodeFromStorage(&s.state, s.dir, blessingStoreDataFile, blessingStoreSigFile, s.signer.PublicKey()); err != nil {
		return nil, err
	}

	for _, mb := range s.state.Store {
		if !reflect.DeepEqual(mb.Blessings.PublicKey(), publicKey) {
			return nil, fmt.Errorf("directory contains Blessings: %v that are not for the provided PublicKey: %v", mb.Blessings, publicKey)
		}
	}
	return s, nil
}
