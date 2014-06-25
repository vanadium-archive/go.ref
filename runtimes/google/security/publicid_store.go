package security

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"veyron2/security"
)

var (
	errStoreAddMismatch = errors.New("public key does not match that of existing PublicIDs in the store")
	errNoMatchingIDs    = errors.New("no matching PublicIDs")
)

func errCombine(err error) error {
	return fmt.Errorf("could not combine matching PublicIDs: %s", err)
}

type taggedIDStore map[security.PublicID][]security.PrincipalPattern

// publicIDStore implements security.PublicIDStore.
type publicIDStore struct {
	// store contains a set of PublicIDs mapped to a set of (peer) patterns. The patterns
	// indicate the set of peers against whom the PublicID can be used. All PublicIDs in
	// the store must have the same public key.
	store taggedIDStore
	// publicKey is the common public key of all PublicIDs held in the store.
	publicKey *ecdsa.PublicKey
	// defaultPattern is the default PrincipalPattern to be used to select
	// PublicIDs from the store in absence of any other search criterea.
	defaultPattern security.PrincipalPattern
	mu             sync.RWMutex
}

func (s *publicIDStore) addTaggedID(id security.PublicID, peerPattern security.PrincipalPattern) {
	switch p := id.(type) {
	case *setPublicID:
		for _, ip := range *p {
			s.addTaggedID(ip, peerPattern)
		}
	default:
		// TODO(ataly): Should we restrict this case to just PublicIDs of type *chainPublicID?
		s.store[id] = append(s.store[id], peerPattern)
	}
}

func (s *publicIDStore) Add(id security.PublicID, peerPattern security.PrincipalPattern) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publicKey != nil && !reflect.DeepEqual(id.PublicKey(), s.publicKey) {
		return errStoreAddMismatch
	}
	if s.publicKey == nil {
		s.publicKey = id.PublicKey()
	}
	s.addTaggedID(id, peerPattern)
	return nil
}

func (s *publicIDStore) ForPeer(peer security.PublicID) (security.PublicID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matchingIDs []security.PublicID
	for id, peerPatterns := range s.store {
		for _, peerPattern := range peerPatterns {
			if peer.Match(peerPattern) {
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
	for id, _ := range s.store {
		if id.Match(s.defaultPattern) {
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

func (s *publicIDStore) SetDefaultPrincipalPattern(pattern security.PrincipalPattern) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// TODO(ataly, ashankar): Should we check that the pattern is well-formed?
	s.defaultPattern = pattern
}

func (s *publicIDStore) String() string {
	var buf bytes.Buffer
	buf.WriteString("&publicIDStore{\n")
	buf.WriteString("  store: {\n")
	for id, peerPatterns := range s.store {
		buf.WriteString(fmt.Sprintf("    %s: %s,\n", id, peerPatterns))
	}
	buf.WriteString(fmt.Sprintf("  },\n"))
	buf.WriteString(fmt.Sprintf("  defaultPattern: %s,\n", s.defaultPattern))
	buf.WriteString("}")
	return buf.String()
}

// NewPublicIDStore returns a new security.PublicIDStore with an empty
// set of PublicIDs, and the default pattern "*" matched by all PublicIDs.
func NewPublicIDStore() security.PublicIDStore {
	return &publicIDStore{
		store:          make(taggedIDStore),
		defaultPattern: security.AllPrincipals,
	}
}
