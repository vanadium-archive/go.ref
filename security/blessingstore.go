package security

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"veyron.io/veyron/veyron/security/serialization"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

var errStoreAddMismatch = errors.New("blessing's public key does not match store's public key")

type blessings struct {
	Value       security.WireBlessings
	unmarshaled security.Blessings
}

func (w *blessings) Blessings() security.Blessings {
	if w == nil {
		return nil
	}
	return w.unmarshaled
}

func (w *blessings) Verify() error {
	var err error
	if w.unmarshaled == nil {
		w.unmarshaled, err = security.NewBlessings(w.Value)
	}
	return err
}

func newWireBlessings(b security.Blessings) *blessings {
	return &blessings{Value: security.MarshalBlessings(b), unmarshaled: b}
}

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
	state      state // GUARDED_BY(mu)
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
		bs.state.Store[forPeers] = newWireBlessings(blessings)
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
	return old.Blessings(), nil
}

func (bs *blessingStore) ForPeer(peerBlessings ...string) security.Blessings {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	var ret security.Blessings
	for pattern, wb := range bs.state.Store {
		if pattern.MatchedBy(peerBlessings...) {
			b := wb.Blessings()
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
	if bs.state.Default != nil {
		return bs.state.Default.Blessings()
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
	bs.state.Default = newWireBlessings(blessings)
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

func (bs *blessingStore) PeerBlessings() map[security.BlessingPattern]security.Blessings {
	m := make(map[security.BlessingPattern]security.Blessings)
	for pattern, wb := range bs.state.Store {
		m[pattern] = wb.Blessings()
	}
	return m
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
	b := bytes.NewBufferString(fmt.Sprintf("Default blessings: %v\n", bs.state.Default.Blessings()))

	b.WriteString(fmt.Sprintf(format, "Peer pattern", "Blessings"))

	sorted := make([]string, 0, len(bs.state.Store))
	for k, _ := range bs.state.Store {
		sorted = append(sorted, string(k))
	}
	sort.Strings(sorted)
	for _, pattern := range sorted {
		wb := bs.state.Store[security.BlessingPattern(pattern)]
		b.WriteString(fmt.Sprintf(format, pattern, wb.Blessings()))
	}
	return b.String()
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
		state:     state{Store: make(map[security.BlessingPattern]*blessings)},
	}
}

// TODO(ataly, ashankar): Get rid of this struct once we have switched all credentials
// directories to the new serialization format.
type oldState struct {
	Store   map[security.BlessingPattern]security.WireBlessings
	Default security.WireBlessings
}

// TODO(ataly, ashankar): Get rid of this method once we have switched all
// credentials directories to the new serialization format.
func (bs *blessingStore) tryOldFormat() bool {
	var empty security.WireBlessings
	if len(bs.state.Store) == 0 {
		return bs.state.Default == nil || reflect.DeepEqual(bs.state.Default.Value, empty)
	}
	for _, wb := range bs.state.Store {
		if len(wb.Value.CertificateChains) == 0 {
			return true
		}
	}
	return false
}

func (bs *blessingStore) verifyState() error {
	verifyBlessings := func(wb *blessings, key security.PublicKey) error {
		if err := wb.Verify(); err != nil {
			return err
		}
		if b := wb.Blessings(); b != nil && !reflect.DeepEqual(b.PublicKey(), key) {
			return fmt.Errorf("read Blessings: %v that are not for provided PublicKey: %v", b, key)
		}
		return nil
	}
	for _, wb := range bs.state.Store {
		if err := verifyBlessings(wb, bs.publicKey); err != nil {
			return err
		}
	}
	if bs.state.Default != nil {
		if err := verifyBlessings(bs.state.Default, bs.publicKey); err != nil {
			return err
		}
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
	var old oldState
	if err := decodeFromStorage(&old, data, signature, bs.signer.PublicKey()); err != nil {
		return err
	}
	for p, wire := range old.Store {
		bs.state.Store[p] = &blessings{Value: wire}
	}
	bs.state.Default = &blessings{Value: old.Default}

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
	if err := decodeFromStorage(&bs.state, data, signature, bs.signer.PublicKey()); err == nil && !bs.tryOldFormat() {
		return bs.verifyState()
	}
	if err := bs.deserializeOld(); err != nil {
		return err
	}
	return nil
}

// newPersistingBlessingStore returns a security.BlessingStore for a principal
// that is initialized with the persisted data. The returned security.BlessingStore
// also persists any updates to its state.
func newPersistingBlessingStore(serializer SerializerReaderWriter, signer serialization.Signer) (security.BlessingStore, error) {
	if serializer == nil || signer == nil {
		return nil, errors.New("persisted data or signer is not specified")
	}
	bs := &blessingStore{
		publicKey:  signer.PublicKey(),
		state:      state{Store: make(map[security.BlessingPattern]*blessings)},
		serializer: serializer,
		signer:     signer,
	}
	if err := bs.deserialize(); err != nil {
		return nil, err
	}
	return bs, nil
}
