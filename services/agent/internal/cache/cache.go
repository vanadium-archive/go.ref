// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"fmt"
	"sync"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/ref/internal/logger"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/internal/lru"
)

const pkgPath = "v.io/x/ref/services/agent/internal/cache"

var (
	errNotImplemented = verror.Register(pkgPath+".errNotImplemented", verror.NoRetry, "{1:}{2:} Not implemented{:_}")
)

const (
	maxNegativeCacheEntries = 100
)

// cachedRoots is a security.BlessingRoots implementation that
// wraps over another implementation and adds caching.
type cachedRoots struct {
	mu    *sync.RWMutex
	impl  security.BlessingRoots
	cache map[string][]security.BlessingPattern // GUARDED_BY(mu)

	// TODO(ataly): Get rid of the following fields once all agents have been
	// updated to support the 'Dump' method.
	dumpExists bool       // GUARDED_BY(my)
	negative   *lru.Cache // key + blessing -> error
}

func newCachedRoots(impl security.BlessingRoots, mu *sync.RWMutex) (*cachedRoots, error) {
	roots := &cachedRoots{mu: mu, impl: impl}
	roots.flush()
	if err := roots.fetchAndCacheRoots(); err != nil {
		return nil, err
	}
	return roots, nil
}

func (r *cachedRoots) Add(root security.PublicKey, pattern security.BlessingPattern) error {
	cacheKey, err := keyToString(root)
	if err != nil {
		return err
	}

	defer r.mu.Unlock()
	r.mu.Lock()

	if err := r.impl.Add(root, pattern); err != nil {
		return err
	}

	if r.cache != nil {

		r.cache[cacheKey] = append(r.cache[cacheKey], pattern)
	}
	return nil
}

func (r *cachedRoots) Recognized(root security.PublicKey, blessing string) (result error) {
	key, err := keyToString(root)
	if err != nil {
		return err
	}

	r.mu.RLock()
	var cacheExists bool
	if r.cache != nil {
		err = r.recognizeFromCache(key, root, blessing)
		cacheExists = true
	}
	r.mu.RUnlock()

	if !cacheExists {
		r.mu.Lock()
		if err := r.fetchAndCacheRoots(); err != nil {
			r.mu.Unlock()
			return err
		}
		err = r.recognizeFromCache(key, root, blessing)
		r.mu.Unlock()
	}

	// TODO(ataly): Get rid of the following block once all agents have been updated
	// to support the 'Dump' method.
	r.mu.RLock()
	if !r.dumpExists && err != nil {
		negKey := key + blessing
		negErr, ok := r.negative.Get(negKey)
		if !ok {
			r.mu.RUnlock()
			return r.recognizeFromImpl(key, root, blessing)
		}
		r.negative.Put(negKey, err)
		err = negErr.(error)
	}
	r.mu.RUnlock()

	return err
}

func (r *cachedRoots) Dump() map[security.BlessingPattern][]security.PublicKey {
	var (
		cacheExists bool
		dump        map[security.BlessingPattern][]security.PublicKey
	)

	r.mu.RLock()
	if r.cache != nil {
		cacheExists = true
		dump = r.dumpFromCache()
	}
	r.mu.RUnlock()

	if !cacheExists {
		r.mu.Lock()
		if err := r.fetchAndCacheRoots(); err != nil {
			logger.Global().Errorf("failed to cache roots: %v", err)
			r.mu.Unlock()
			return nil
		}
		dump = r.dumpFromCache()
		r.mu.Unlock()
	}
	return dump
}

func (r *cachedRoots) DebugString() string {
	return r.impl.DebugString()
}

// Must be called while holding mu.
func (r *cachedRoots) fetchAndCacheRoots() error {
	dump := r.impl.Dump()
	r.cache = make(map[string][]security.BlessingPattern)
	if dump == nil {
		r.dumpExists = false
		return nil
	}

	for p, keys := range dump {
		for _, key := range keys {
			cacheKey, err := keyToString(key)
			if err != nil {
				return err
			}
			r.cache[cacheKey] = append(r.cache[cacheKey], p)
		}
	}
	r.dumpExists = true
	return nil
}

// Must be called while holding mu.
func (r *cachedRoots) flush() {
	r.cache = nil
	r.negative = lru.New(maxNegativeCacheEntries)
}

// Must be called while holding mu.
func (r *cachedRoots) dumpFromCache() map[security.BlessingPattern][]security.PublicKey {
	if !r.dumpExists {
		return nil
	}
	dump := make(map[security.BlessingPattern][]security.PublicKey)
	for keyStr, patterns := range r.cache {
		key, err := security.UnmarshalPublicKey([]byte(keyStr))
		if err != nil {
			logger.Global().Errorf("security.UnmarshalPublicKey(%v) returned error: %v", []byte(keyStr), err)
			return nil
		}
		for _, p := range patterns {
			dump[p] = append(dump[p], key)
		}
	}
	return dump
}

// Must be called while holding mu.
func (r *cachedRoots) recognizeFromCache(key string, root security.PublicKey, blessing string) error {
	for _, p := range r.cache[key] {
		if p.MatchedBy(blessing) {
			return nil
		}
	}
	return security.NewErrUnrecognizedRoot(nil, root.String(), nil)
}

// TODO(ataly): Get rid of this method once all agents have been updated
// to support the 'Dump' method.
func (r *cachedRoots) recognizeFromImpl(key string, root security.PublicKey, blessing string) error {
	negKey := key + blessing
	err := r.impl.Recognized(root, blessing)

	r.mu.Lock()
	if err == nil {
		r.cache[key] = append(r.cache[key], security.BlessingPattern(blessing))
	} else {
		r.negative.Put(negKey, err)
	}
	r.mu.Unlock()
	return err
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
				logger.Global().Errorf("UnionOfBlessings(%v, %v) failed: %v, dropping the latter from BlessingStore.ForPeers(%v)", ret, b, err, peerBlessings)
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

func (s *cachedStore) CacheDischarge(d security.Discharge, c security.Caveat, i security.DischargeImpetus) {
	s.mu.Lock()
	s.impl.CacheDischarge(d, c, i)
	s.mu.Unlock()
}

func (s *cachedStore) ClearDischarges(discharges ...security.Discharge) {
	s.mu.Lock()
	s.impl.ClearDischarges(discharges...)
	s.mu.Unlock()
}

func (s *cachedStore) Discharge(caveat security.Caveat, impetus security.DischargeImpetus) security.Discharge {
	defer s.mu.Unlock()
	s.mu.Lock()
	return s.impl.Discharge(caveat, impetus)
}

// Must be called while holding mu.
func (s *cachedStore) flush() {
	s.hasDef = false
	s.peers = nil
}

// cachedPrincipal is a security.Principal implementation that
// wraps over another implementation and adds caching.
type cachedPrincipal struct {
	cache security.Principal
	/* impl */ agent.Principal
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

func (p *cachedPrincipal) Close() error {
	return p.Principal.Close()
}

type dummySigner struct {
	key security.PublicKey
}

func (s dummySigner) Sign(purpose, message []byte) (security.Signature, error) {
	var sig security.Signature
	return sig, verror.New(errNotImplemented, nil)
}

func (s dummySigner) PublicKey() security.PublicKey {
	return s.key
}

func NewCachedPrincipal(ctx *context.T, impl agent.Principal, call rpc.ClientCall) (p agent.Principal, err error) {
	p, flush, err := NewCachedPrincipalX(impl)

	if err == nil {
		go func() {
			var x bool
			for {
				if recvErr := call.Recv(&x); recvErr != nil {
					if ctx.Err() != context.Canceled {
						logger.Global().Errorf("Error from agent: %v", recvErr)
					}
					flush()
					call.Finish()
					return
				}
				flush()
			}
		}()
	}

	return
}

func NewCachedPrincipalX(impl agent.Principal) (p agent.Principal, flush func(), err error) {
	var mu sync.RWMutex
	cachedRoots, err := newCachedRoots(impl.Roots(), &mu)
	if err != nil {
		return
	}
	cachedStore := &cachedStore{
		mu:   &mu,
		key:  impl.PublicKey(),
		impl: impl.BlessingStore(),
	}
	flush = func() {
		defer mu.Unlock()
		mu.Lock()
		cachedRoots.flush()
		cachedStore.flush()
	}
	sp, err := security.CreatePrincipal(dummySigner{impl.PublicKey()}, cachedStore, cachedRoots)
	if err != nil {
		return
	}

	p = &cachedPrincipal{sp, impl}
	return
}

func keyToString(key security.PublicKey) (string, error) {
	bytes, err := key.MarshalBinary()
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
