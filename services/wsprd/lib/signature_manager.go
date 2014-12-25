package lib

import (
	"sync"
	"time"

	"v.io/veyron/veyron2/context"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/ipc/reserved"
	"v.io/veyron/veyron2/vdl/vdlroot/src/signature"
	"v.io/veyron/veyron2/verror2"
)

type SignatureManager interface {
	Signature(ctx context.T, name string, client ipc.Client, opts ...ipc.CallOpt) ([]signature.Interface, error)
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
}

// NewSignatureManager creates and initialized a new Signature Manager
func NewSignatureManager() SignatureManager {
	return &signatureManager{cache: make(map[string]*cacheEntry)}
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

// Signature uses the given client to fetch the signature for the given service
// name.  It either returns the signature from the cache, or blocks until it
// fetches the signature from the remote server.
func (sm *signatureManager) Signature(ctx context.T, name string, client ipc.Client, opts ...ipc.CallOpt) ([]signature.Interface, error) {
	sm.Lock()
	defer sm.Unlock()

	if entry := sm.cache[name]; entry != nil && !entry.expired() {
		entry.lastAccessed = time.Now()
		return entry.sig, nil
	}

	// Fetch from the remote server.
	sig, err := reserved.Signature(ctx, client, name, opts...)
	if err != nil {
		return nil, verror2.Make(verror2.NoServers, ctx, name, err)
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
