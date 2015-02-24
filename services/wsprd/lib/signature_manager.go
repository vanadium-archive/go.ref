package lib

import (
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/ipc/reserved"
	"v.io/v23/vdl/vdlroot/src/signature"
	"v.io/v23/verror"
)

type SignatureManager interface {
	Signature(ctx *context.T, name string, opts ...ipc.CallOpt) ([]signature.Interface, error)
	FlushCacheEntry(name string)
}

// signatureManager can be used to discover the signature of a remote service
// It has built-in caching and TTL support.
type signatureManager struct {
	// protects the cache and initialization
	sync.Mutex

	// map of name to service signature and last-accessed time
	// TODO(aghassemi) GC for expired cache entries
	cache map[string]*cacheEntry

	// we keep track of current pending request so we don't issue
	// multiple signature requests to the same server simultaneously.
	pendingSignatures map[string]chan struct{}
}

// NewSignatureManager creates and initialized a new Signature Manager
func NewSignatureManager() SignatureManager {
	return &signatureManager{
		cache:             make(map[string]*cacheEntry),
		pendingSignatures: map[string]chan struct{}{},
	}
}

const (
	// ttl from the last-accessed time.
	ttl = time.Duration(time.Hour)
)

type cacheEntry struct {
	sig          []signature.Interface
	lastAccessed time.Time
}

// expired returns whether the cache entry is expired or not
func (c cacheEntry) expired() bool {
	return time.Now().Sub(c.lastAccessed) > ttl
}

const pkgPath = "v.io/wspr/veyron/services/wsprd/lib"

func (sm *signatureManager) lookupCacheLocked(name string) []signature.Interface {
	if entry := sm.cache[name]; entry != nil && !entry.expired() {
		entry.lastAccessed = time.Now()
		return entry.sig
	}
	return nil
}

// Signature fetches the signature for the given service name.  It either
// returns the signature from the cache, or blocks until it fetches the
// signature from the remote server.
func (sm *signatureManager) Signature(ctx *context.T, name string, opts ...ipc.CallOpt) ([]signature.Interface, error) {
	sm.Lock()

	if sigs := sm.lookupCacheLocked(name); sigs != nil {
		sm.Unlock()
		return sigs, nil
	}

	ch, found := sm.pendingSignatures[name]

	if !found {
		ch = make(chan struct{})
		sm.pendingSignatures[name] = ch
	}
	sm.Unlock()

	if found {
		<-ch
		// If the channel is closed then we know that the outstanding request finished
		// if it failed then there will not be a valid entry in the cache.
		sm.Lock()
		result := sm.lookupCacheLocked(name)
		sm.Unlock()
		var err error
		if result == nil {
			return nil, verror.New(verror.ErrNoServers, ctx, name, err)

		}
		return result, nil
	}

	// Fetch from the remote server.
	sig, err := reserved.Signature(ctx, name, opts...)
	sm.Lock()
	// On cleanup we need to close the channel, remove the entry from the
	defer func() {
		close(ch)
		delete(sm.pendingSignatures, name)
		sm.Unlock()
	}()
	if err != nil {
		return nil, verror.New(verror.ErrNoServers, ctx, name, err)
	}

	// Add to the cache.
	sm.cache[name] = &cacheEntry{
		sig:          sig,
		lastAccessed: time.Now(),
	}
	return sig, nil
}

// FlushCacheEntry removes the cached signature for the given name
func (sm *signatureManager) FlushCacheEntry(name string) {
	delete(sm.cache, name)
}
