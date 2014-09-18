package security

import (
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"sync"

	"veyron.io/veyron/veyron/security/serialization"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

const (
	dataFile      = "blessingstore.data"
	signatureFile = "blessingstore.sig"
)

var (
	errStoreAddMismatch = errors.New("public key does not match that of existing PublicIDs in the store")
	errNoMatchingIDs    = errors.New("no matching PublicIDs")
)

func errCombine(err error) error {
	return fmt.Errorf("could not combine matching PublicIDs: %s", err)
}

func saveErr(err error) error {
	return fmt.Errorf("could not save PublicIDStore: %s", err)
}

type taggedIDStore map[security.PublicID][]security.BlessingPattern

type persistentState struct {
	// Store contains a set of PublicIDs mapped to a set of (peer) patterns. The
	// patterns indicate the set of peers against whom the PublicID can be used.
	// All PublicIDs in the store must have the same public key.
	Store taggedIDStore
	// DefaultPattern is the default BlessingPattern to be used to select
	// PublicIDs from the store in absence of any other search criterea.
	DefaultPattern security.BlessingPattern
}

// publicIDStore implements security.PublicIDStore.
type publicIDStore struct {
	state     persistentState
	publicKey security.PublicKey
	params    *PublicIDStoreParams
	mu        sync.RWMutex
}

func (s *publicIDStore) addTaggedID(id security.PublicID, peerPattern security.BlessingPattern) ([]security.PublicID, error) {
	var updatedIDs []security.PublicID
	switch p := id.(type) {
	case *setPublicID:
		for _, ip := range *p {
			ids, err := s.addTaggedID(ip, peerPattern)
			if err != nil {
				return updatedIDs, err
			}
			updatedIDs = append(updatedIDs, ids...)
		}
	default:
		// TODO(ataly): Once we get rid of FakePublicID, force this case to be exactly
		// *chainPublicID.
		for _, pattern := range s.state.Store[id] {
			if pattern == peerPattern {
				return updatedIDs, nil
			}
		}
		s.state.Store[id] = append(s.state.Store[id], peerPattern)
		updatedIDs = append(updatedIDs, id)
	}
	return updatedIDs, nil
}

func (s *publicIDStore) revert(updatedIDs []security.PublicID) {
	for _, id := range updatedIDs {
		s.state.Store[id] = s.state.Store[id][:len(s.state.Store[id])-1]
	}
}

func (s *publicIDStore) Add(id security.PublicID, peerPattern security.BlessingPattern) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	publicKeyIsNil := s.publicKey == nil
	if !publicKeyIsNil && !reflect.DeepEqual(id.PublicKey(), s.publicKey) {
		return errStoreAddMismatch
	}
	if publicKeyIsNil {
		s.publicKey = id.PublicKey()
	}

	updatedIDs, err := s.addTaggedID(id, peerPattern)
	if err != nil {
		s.revert(updatedIDs)
		return err
	}

	if err := s.save(); err != nil {
		s.revert(updatedIDs)
		if publicKeyIsNil {
			s.publicKey = nil
		}
		return saveErr(err)
	}
	return nil
}

func (s *publicIDStore) ForPeer(peer security.PublicID) (security.PublicID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matchingIDs []security.PublicID
	for id, peerPatterns := range s.state.Store {
		for _, peerPattern := range peerPatterns {
			if peerPattern.MatchedBy(peer.Names()...) {
				matchingIDs = append(matchingIDs, id)
				break
			}
		}
	}
	id, err := NewSetPublicID(matchingIDs...)
	if err != nil {
		return nil, errCombine(err)
	}
	if id == nil {
		return nil, errNoMatchingIDs
	}
	return id, nil
}

func (s *publicIDStore) DefaultPublicID() (security.PublicID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matchingIDs []security.PublicID
	for id, _ := range s.state.Store {
		if s.state.DefaultPattern.MatchedBy(id.Names()...) {
			matchingIDs = append(matchingIDs, id)
		}
	}
	id, err := NewSetPublicID(matchingIDs...)
	if err != nil {
		return nil, errCombine(err)
	}
	if id == nil {
		return nil, errNoMatchingIDs
	}
	return id, nil
}

func (s *publicIDStore) SetDefaultBlessingPattern(pattern security.BlessingPattern) error {
	if !pattern.IsValid() {
		return fmt.Errorf("%q is an invalid BlessingPattern", pattern)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	oldPattern := s.state.DefaultPattern
	s.state.DefaultPattern = pattern

	if err := s.save(); err != nil {
		s.state.DefaultPattern = oldPattern
		return saveErr(err)
	}
	return nil
}

func (s *publicIDStore) save() error {
	if s.params == nil {
		return nil
	}

	// Save the state to temporary data and signature files, and then move
	// those files to the actually data and signature file. This reduces the
	// risk of loosing all saved data on disk in the event of a Write failure.
	dataPath := path.Join(s.params.Dir, dataFile)
	tempDataPath := dataPath + "_tmp"
	sigPath := path.Join(s.params.Dir, signatureFile)
	tempSigPath := sigPath + "_tmp"

	data, err := os.OpenFile(tempDataPath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(tempDataPath)
	sig, err := os.OpenFile(tempSigPath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(tempSigPath)

	swc, err := serialization.NewSigningWriteCloser(data, sig, s.params.Signer, nil)
	if err != nil {
		return err
	}
	if err := vom.NewEncoder(swc).Encode(s.state); err != nil {
		defer swc.Close()
		return err
	}
	if err := swc.Close(); err != nil {
		return err
	}

	if err := os.Rename(tempDataPath, dataPath); err != nil {
		return err
	}
	return os.Rename(tempSigPath, sigPath)
}

func (s *publicIDStore) String() string {
	return fmt.Sprintf("{state: %v, params: %v}", s.state, s.params)
}

// PublicIDStoreParams specifies persistent storage where a PublicIDStore can be
// saved and loaded.
type PublicIDStoreParams struct {
	// Dir specifies a path to a directory in which a serialized PublicIDStore
	// can be saved and loaded.
	Dir string
	// Signer provides a mechanism to sign and verify the serialized bytes.
	Signer serialization.Signer
}

// NewPublicIDStore returns a security.PublicIDStore based on params.
// * If params is nil, a new store with an empty set of PublicIDs and the default
//   pattern "..." (matched by all PublicIDs) is returned. The store only lives in
//   memory and is never persisted.
// * If params is non-nil, then a store obtained from the serialized data present
//   in params.Dir is returned if the data exists, or else a new store with an
//   empty set of PublicIDs and the default pattern "..." is returned. Any subsequent
//   modifications to the returned store are always signed (using params.Signer)
//   and persisted in params.Dir.
func NewPublicIDStore(params *PublicIDStoreParams) (security.PublicIDStore, error) {
	store := &publicIDStore{
		state:  persistentState{make(taggedIDStore), security.AllPrincipals},
		params: params,
	}
	if store.params == nil {
		return store, nil
	}

	data, dataErr := os.Open(path.Join(store.params.Dir, dataFile))
	defer data.Close()
	sig, sigErr := os.Open(path.Join(store.params.Dir, signatureFile))
	defer sig.Close()

	switch {
	case os.IsNotExist(dataErr) && os.IsNotExist(sigErr):
		// No params exists, returning an empty PublicIDStore.
		return store, nil
	case dataErr != nil:
		return nil, dataErr
	case sigErr != nil:
		return nil, sigErr
	}

	vr, err := serialization.NewVerifyingReader(data, sig, store.params.Signer.PublicKey())
	if err != nil {
		return nil, err
	}
	if err := vom.NewDecoder(vr).Decode(&store.state); err != nil {
		return nil, err
	}

	for id, _ := range store.state.Store {
		store.publicKey = id.PublicKey()
		break
	}
	return store, nil
}
