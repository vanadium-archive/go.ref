package security

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"veyron.io/veyron/veyron/security/serialization"

	"veyron.io/veyron/veyron2/security"
)

const (
	blessingRootsDataFile = "blessingroots.data"
	blessingRootsSigFile  = "blessingroots.sig"
)

// blessingRoots implements security.BlessingRoots.
type blessingRoots struct {
	dir    string
	signer serialization.Signer
	mu     sync.RWMutex
	store  map[string][]security.BlessingPattern // GUARDED_BY(mu)
}

func storeMapKey(root security.PublicKey) (string, error) {
	rootBytes, err := root.MarshalBinary()
	if err != nil {
		return "", err
	}
	return string(rootBytes), nil
}

func (br *blessingRoots) Add(root security.PublicKey, pattern security.BlessingPattern) error {
	key, err := storeMapKey(root)
	if err != nil {
		return err
	}

	br.mu.Lock()
	defer br.mu.Unlock()
	patterns := br.store[key]
	for _, p := range patterns {
		if p == pattern {
			return nil
		}
	}
	br.store[key] = append(patterns, pattern)

	if err := br.save(); err != nil {
		br.store[key] = patterns[:len(patterns)-1]
		return err
	}
	return nil
}

func (br *blessingRoots) Recognized(root security.PublicKey, blessing string) error {
	key, err := storeMapKey(root)
	if err != nil {
		return err
	}

	br.mu.RLock()
	defer br.mu.RUnlock()
	for _, p := range br.store[key] {
		if p.MatchedBy(blessing) {
			return nil
		}
	}
	return errors.New("PublicKey is not a recognized root for this blessing")
}

// DebugString return a human-readable string encoding of the roots
// DebugString encodes all roots into a string in the following
// format
//
// Public key   : Pattern
// <public key> : <pattern>
// ...
// <public key> : <pattern>
func (br *blessingRoots) DebugString() string {
	const format = "%-47s : %s\n"
	b := bytes.NewBufferString(fmt.Sprintf(format, "Public key", "Pattern"))
	for keyBytes, pattern := range br.store {
		key, err := security.UnmarshalPublicKey([]byte(keyBytes))
		if err != nil {
			return fmt.Sprintf("failed to decode public key: %v", err)
		}
		b.WriteString(fmt.Sprintf(format, key, pattern))
	}
	return b.String()
}

func (br *blessingRoots) save() error {
	if (br.signer == nil) && (br.dir == "") {
		return nil
	}
	return encodeAndStore(br.store, br.dir, blessingRootsDataFile, blessingRootsSigFile, br.signer)
}

// newInMemoryBlessingRoots returns an in-memory security.BlessingRoots.
//
// The returned BlessingRoots is initialized with an empty set of keys.
func newInMemoryBlessingRoots() security.BlessingRoots {
	return &blessingRoots{
		store: make(map[string][]security.BlessingPattern),
	}
}

// newPersistingBlessingRoots returns a security.BlessingRoots that signs
// and persists all updates to the provided directory. Signing is carried
// out using the provided signer.
//
// The returned BlessingRoots is initialized from the existing data present
// in the directory. The data is verified to have been written by a persisting
// BlessingRoots object constructed from the same signer.
//
// Any errors obtained in reading or verifying the data are returned.
func newPersistingBlessingRoots(directory string, signer serialization.Signer) (security.BlessingRoots, error) {
	if directory == "" || signer == nil {
		return nil, errors.New("directory or signer is not specified")
	}
	br := &blessingRoots{
		store:  make(map[string][]security.BlessingPattern),
		dir:    directory,
		signer: signer,
	}

	if err := decodeFromStorage(&br.store, br.dir, blessingRootsDataFile, blessingRootsSigFile, br.signer.PublicKey()); err != nil {
		return nil, err
	}
	return br, nil
}
