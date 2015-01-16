package ipc

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sync"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/security"
)

// clientEncodeBlessings gets or inserts the blessings into the cache.
func clientEncodeBlessings(cache stream.VCDataCache, blessings security.Blessings) ipc.BlessingsRequest {
	blessingsCacheAny := cache.GetOrInsert(clientBlessingsCacheKey{}, newClientBlessingsCache)
	blessingsCache := blessingsCacheAny.(*clientBlessingsCache)
	return blessingsCache.getOrInsert(blessings)
}

// clientAckBlessings verifies that the server has updated its cache to include blessings.
// This means that subsequent rpcs from the client with blessings can send only a cache key.
func clientAckBlessings(cache stream.VCDataCache, blessings security.Blessings) {
	blessingsCacheAny := cache.GetOrInsert(clientBlessingsCacheKey{}, newClientBlessingsCache)
	blessingsCache := blessingsCacheAny.(*clientBlessingsCache)
	blessingsCache.acknowledge(blessings)
}

// serverDecodeBlessings insert the key and blessings into the cache or get the blessings if only
// key is provided in req.
func serverDecodeBlessings(cache stream.VCDataCache, req ipc.BlessingsRequest, stats *ipcStats) (security.Blessings, error) {
	blessingsCacheAny := cache.GetOrInsert(serverBlessingsCacheKey{}, newServerBlessingsCache)
	blessingsCache := blessingsCacheAny.(*serverBlessingsCache)
	return blessingsCache.getOrInsert(req, stats)
}

// IMPLEMENTATION DETAILS BELOW

// clientBlessingsCache is a thread-safe map from blessings to cache id.
type clientBlessingsCache struct {
	sync.RWMutex
	m     map[[sha256.Size]byte]clientCacheValue
	curId uint64
}

type clientCacheValue struct {
	id uint64
	// ack is set to true once the server has confirmed receipt of the cache id.
	// Clients that insert into the cache when ack is false must send both the id
	// and the blessings.
	ack bool
}

// clientBlessingsCacheKey is the key used to retrieve the clientBlessingsCache from the VCDataCache.
type clientBlessingsCacheKey struct{}

func newClientBlessingsCache() interface{} {
	return &clientBlessingsCache{m: make(map[[sha256.Size]byte]clientCacheValue)}
}

func getBlessingsHashKey(blessings security.Blessings) (key [sha256.Size]byte) {
	h := sha256.New()
	for _, chain := range security.MarshalBlessings(blessings).CertificateChains {
		if len(chain) == 0 {
			continue
		}
		cert := chain[len(chain)-1]
		s := sha256.Sum256(cert.Signature.R)
		h.Write(s[:])
		s = sha256.Sum256(cert.Signature.S)
		h.Write(s[:])
	}
	copy(key[:], h.Sum(nil))
	return
}

func (c *clientBlessingsCache) getOrInsert(blessings security.Blessings) ipc.BlessingsRequest {
	key := getBlessingsHashKey(blessings)
	c.RLock()
	val, exists := c.m[key]
	c.RUnlock()
	if exists {
		return c.makeBlessingsRequest(val, blessings)
	}
	// If the val doesn't exist we must create a new id, update the cache, and send the id and blessings.
	c.Lock()
	// We must check that the val wasn't inserted in the time we changed locks.
	val, exists = c.m[key]
	if !exists {
		val = clientCacheValue{id: c.nextIdLocked()}
		c.m[key] = val
	}
	c.Unlock()
	return c.makeBlessingsRequest(val, blessings)
}

func (c *clientBlessingsCache) acknowledge(blessings security.Blessings) {
	key := getBlessingsHashKey(blessings)
	c.Lock()
	val := c.m[key]
	val.ack = true
	c.m[key] = val
	c.Unlock()
}

func (c *clientBlessingsCache) makeBlessingsRequest(val clientCacheValue, blessings security.Blessings) ipc.BlessingsRequest {
	if val.ack {
		// when the value is acknowledged, only send the key, since the server has confirmed that it knows the key.
		return ipc.BlessingsRequest{Key: val.id}
	}
	// otherwise we still need to send both key and blessings, but we must ensure that we send the same key.
	wireBlessings := security.MarshalBlessings(blessings)
	return ipc.BlessingsRequest{val.id, &wireBlessings}
}

// nextIdLocked creates a new id for inserting blessings. It must be called after acquiring a writer lock.
func (c *clientBlessingsCache) nextIdLocked() uint64 {
	c.curId++
	return c.curId
}

// serverBlessingsCache is a thread-safe map from cache key to blessings.
type serverBlessingsCache struct {
	sync.RWMutex
	m map[uint64]security.Blessings
}

// serverBlessingsCacheKey is the key used to retrieve the serverBlessingsCache from the VCDataCache.
type serverBlessingsCacheKey struct{}

func newServerBlessingsCache() interface{} {
	return &serverBlessingsCache{m: make(map[uint64]security.Blessings)}
}

func (c *serverBlessingsCache) getOrInsert(req ipc.BlessingsRequest, stats *ipcStats) (security.Blessings, error) {
	// In the case that the key sent is 0, we are running in VCSecurityNone and should
	// return nil for the client Blessings.
	if req.Key == 0 {
		return nil, nil
	}
	if req.Blessings == nil {
		// Fastpath, lookup based on the key.
		c.RLock()
		cached, exists := c.m[req.Key]
		c.RUnlock()
		if !exists {
			return nil, fmt.Errorf("ipc: key was not in the cache")
		}
		stats.recordBlessingCache(true)
		return cached, nil
	}
	// Slowpath, might need to update the cache, or check that the received blessings are
	// the same as what's in the cache.
	recv, err := security.NewBlessings(*req.Blessings)
	if err != nil {
		return nil, fmt.Errorf("ipc: create new client blessings failed: %v", err)
	}
	c.Lock()
	defer c.Unlock()
	if cached, exists := c.m[req.Key]; exists {
		// TODO(suharshs): Replace this reflect.DeepEqual() with a less expensive check.
		if !reflect.DeepEqual(cached, recv) {
			return nil, fmt.Errorf("client sent invalid Blessings")
		}
		stats.recordBlessingCache(true)
		return cached, nil
	}
	c.m[req.Key] = recv
	stats.recordBlessingCache(false)
	return recv, nil
}
