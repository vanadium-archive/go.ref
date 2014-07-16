package lib

import (
	"sync"
	"time"

	"veyron2/context"
	"veyron2/ipc"
)

type SignatureManager interface {
	Signature(ctx context.T, name string, client ipc.Client) (*ipc.ServiceSignature, error)
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
	signature    ipc.ServiceSignature
	lastAccessed time.Time
}

// expired returns whether the cache entry is expired or not
func (c cacheEntry) expired() bool {
	return time.Now().Sub(c.lastAccessed) > ttl
}

// signature uses the given client to fetch the signature for the given service name.
// It locks until it fetches the service signature from the remote server, if not a cache hit.
func (sm *signatureManager) Signature(ctx context.T, name string, client ipc.Client) (*ipc.ServiceSignature, error) {
	sm.Lock()
	defer sm.Unlock()

	if cashedSig := sm.cache[name]; cashedSig != nil && !cashedSig.expired() {
		cashedSig.lastAccessed = time.Now()
		return &cashedSig.signature, nil
	}

	// cache expired or not found, fetch it from the remote server
	signatureCall, err := client.StartCall(ctx, name, "Signature", []interface{}{})
	if err != nil {
		return nil, err
	}

	var result ipc.ServiceSignature
	var appErr error
	if err := signatureCall.Finish(&result, &appErr); err != nil {
		return nil, err
	}
	if appErr != nil {
		return nil, appErr
	}

	// cache the result
	sm.cache[name] = &cacheEntry{
		signature:    result,
		lastAccessed: time.Now(),
	}

	return &result, nil
}
