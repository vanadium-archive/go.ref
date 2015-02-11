package cache

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sync"

	"v.io/core/veyron/security/agent/lru"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"
)

const (
	maxNegativeCacheEntries = 100
)

// cachedRoots is a security.BlessingRoots implementation that
// wraps over another implementation and adds caching.
// It caches both known roots, and a blessings known to be untrusted.
// Only the negative cache is cleared when flushing, since BlessingRoots
// doesn't allow removal.
type cachedRoots struct {
	mu       *sync.RWMutex
	impl     security.BlessingRoots
	cache    map[string][]security.BlessingPattern
	negative *lru.Cache // key + blessing -> error
}

func newCachedRoots(impl security.BlessingRoots, mu *sync.RWMutex) *cachedRoots {
	roots := &cachedRoots{mu: mu, impl: impl}
	roots.flush()
	return roots
}

// Must be called while holding mu.
func (r *cachedRoots) flush() {
	r.negative = lru.New(maxNegativeCacheEntries)
	r.cache = make(map[string][]security.BlessingPattern)
}

func (r *cachedRoots) Add(root security.PublicKey, pattern security.BlessingPattern) error {
	cacheKey, err := keyToString(root)
	if err != nil {
		return err
	}

	defer r.mu.Unlock()
	r.mu.Lock()

	err = r.impl.Add(root, pattern)
	if err == nil {
		r.cache[cacheKey] = append(r.cache[cacheKey], pattern)
	}
	return err
}

func keyToString(key security.PublicKey) (string, error) {
	bytes, err := key.MarshalBinary()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

func (r *cachedRoots) Recognized(root security.PublicKey, blessing string) (result error) {
	key, err := keyToString(root)
	if err != nil {
		return err
	}

	r.mu.RLock()

	for _, p := range r.cache[key] {
		if p.MatchedBy(blessing) {
			r.mu.RUnlock()
			return nil
		}
	}
	r.mu.RUnlock()
	r.mu.Lock()
	negKey := key + blessing
	if err, ok := r.negative.Get(negKey); ok {
		r.negative.Put(negKey, err)
		r.mu.Unlock()
		return err.(error)
	}
	r.mu.Unlock()
	return r.recognizeAndCache(key, root, blessing)
}

func (r *cachedRoots) recognizeAndCache(key string, root security.PublicKey, blessing string) error {
	err := r.impl.Recognized(root, blessing)
	r.mu.Lock()
	if err == nil {
		r.cache[key] = append(r.cache[key], security.BlessingPattern(blessing))
	} else {
		r.negative.Put(key+blessing, err)
	}
	r.mu.Unlock()
	return err
}

func (r cachedRoots) DebugString() string {
	return r.impl.DebugString()
}

// cachedStore is a security.BlessingStore implementation that
// wraps over another implementation and adds caching.
type cachedStore struct {
	mu     *sync.RWMutex
	key    security.PublicKey
	def    security.Blessings
	hasDef bool
	peers  map[security.BlessingPattern]security.Blessings
	impl   security.BlessingStore
}

func (s *cachedStore) Default() (result security.Blessings) {
	s.mu.RLock()
	if !s.hasDef {
		s.mu.RUnlock()
		return s.fetchAndCacheDefault()
	}
	result = s.def
	s.mu.RUnlock()
	return
}

func (s *cachedStore) SetDefault(blessings security.Blessings) error {
	defer s.mu.Unlock()
	s.mu.Lock()
	err := s.impl.SetDefault(blessings)
	if err != nil {
		// We're not sure what happened, so we need to re-read the default.
		s.hasDef = false
		return err
	}
	s.def = blessings
	s.hasDef = true
	return nil
}

func (s *cachedStore) fetchAndCacheDefault() security.Blessings {
	result := s.impl.Default()
	s.mu.Lock()
	s.def = result
	s.hasDef = true
	s.mu.Unlock()
	return result
}

func (s *cachedStore) ForPeer(peerBlessings ...string) security.Blessings {
	var ret security.Blessings
	for pat, b := range s.PeerBlessings() {
		if pat.MatchedBy(peerBlessings...) {
			if union, err := security.UnionOfBlessings(ret, b); err == nil {
				ret = union
			} else {
				vlog.Errorf("UnionOfBlessings(%v, %v) failed: %v, dropping the latter from BlessingStore.ForPeers(%v)", ret, b, err, peerBlessings)
			}
		}
	}
	return ret
}

func (s *cachedStore) PeerBlessings() map[security.BlessingPattern]security.Blessings {
	s.mu.RLock()
	ret := s.peers
	s.mu.RUnlock()
	if ret != nil {
		return ret
	}
	return s.fetchAndCacheBlessings()
}

func (s *cachedStore) Set(blessings security.Blessings, forPeers security.BlessingPattern) (security.Blessings, error) {
	defer s.mu.Unlock()
	s.mu.Lock()
	oldBlessings, err := s.impl.Set(blessings, forPeers)
	if err == nil && s.peers != nil {
		s.peers[forPeers] = blessings
	}
	return oldBlessings, err
}

func (s *cachedStore) fetchAndCacheBlessings() map[security.BlessingPattern]security.Blessings {
	ret := s.impl.PeerBlessings()
	s.mu.Lock()
	s.peers = ret
	s.mu.Unlock()
	return ret
}

func (s *cachedStore) PublicKey() security.PublicKey {
	return s.key
}

func (s *cachedStore) DebugString() string {
	return s.impl.DebugString()
}

func (s *cachedStore) String() string {
	return fmt.Sprintf("cached[%s]", s.impl)
}

// Must be called while holding mu
func (s *cachedStore) flush() {
	s.hasDef = false
	s.peers = nil
}

// cachedPrincipal is a security.Principal implementation that
// wraps over another implementation and adds caching.
type cachedPrincipal struct {
	cache security.Principal
	/* impl */ security.Principal
}

func (p *cachedPrincipal) BlessingsByName(pattern security.BlessingPattern) []security.Blessings {
	return p.cache.BlessingsByName(pattern)
}

func (p *cachedPrincipal) BlessingsInfo(blessings security.Blessings) map[string][]security.Caveat {
	return p.cache.BlessingsInfo(blessings)
}

func (p *cachedPrincipal) BlessingStore() security.BlessingStore {
	return p.cache.BlessingStore()
}

func (p *cachedPrincipal) Roots() security.BlessingRoots {
	return p.cache.Roots()
}

func (p *cachedPrincipal) AddToRoots(blessings security.Blessings) error {
	return p.cache.AddToRoots(blessings)
}

type dummySigner struct {
	key security.PublicKey
}

func (s dummySigner) Sign(purpose, message []byte) (security.Signature, error) {
	var sig security.Signature
	return sig, errors.New("Not implemented")
}

func (s dummySigner) PublicKey() security.PublicKey {
	return s.key
}

func NewCachedPrincipal(ctx *context.T, impl security.Principal, call ipc.Call) (p security.Principal, err error) {
	var mu sync.RWMutex
	cachedRoots := newCachedRoots(impl.Roots(), &mu)
	cachedStore := &cachedStore{mu: &mu, key: impl.PublicKey(), impl: impl.BlessingStore()}
	flush := func() {
		defer mu.Unlock()
		mu.Lock()
		cachedRoots.flush()
		cachedStore.flush()
	}
	p, err = security.CreatePrincipal(dummySigner{impl.PublicKey()}, cachedStore, cachedRoots)
	if err != nil {
		return
	}
	go func() {
		var x bool
		for {
			if recvErr := call.Recv(&x); recvErr != nil {
				if ctx.Err() != context.Canceled {
					vlog.Infof("Error from agent: %v", recvErr)
				}
				flush()
				call.Finish()
				return
			}
			flush()
		}
	}()
	p = &cachedPrincipal{p, impl}
	return
}
