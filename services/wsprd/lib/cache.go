package lib

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"log"
	"sync"

	"veyron/runtimes/google/lib/lru"
	"veyron2/ipc"
	"veyron2/security"
	"veyron2/vom"
)

// ClientCache is a concurrent-use, type-safe wrapper over lru.Cache
// where keys are (hashes of) identity.PrivateID and values are
// ipc.Client.
type ClientCache struct {
	sync.Mutex
	// TODO(bjornick): Write our own lru cache that doesn't remove an entry
	// on get.
	cache *lru.Cache

	nPuts, nGets, nHits, nEvicts int64
}

// NewClientCache returns a cache with a total of size entries.
func NewClientCache(size int) *ClientCache {
	return &ClientCache{cache: lru.New(size)}
}

// Put stores the client for the passed in identity.
func (c *ClientCache) Put(id security.PrivateID, client ipc.Client) {
	key, err := c.key(id)
	if err != nil {
		log.Println("ERROR creating cache key for identity:", id, ", error:", err)
		return
	}
	c.Lock()
	c.nPuts++
	_, evictedValue, evicted := c.cache.Put(key, client)
	if evicted {
		c.nEvicts++
	}
	c.Unlock()
	if evicted {
		evictedValue.(ipc.Client).Close()
	}
}

// Get returns the cached client for id.  If there is no entry, it returns nil.
func (c *ClientCache) Get(id security.PrivateID) ipc.Client {
	key, err := c.key(id)
	if err != nil {
		log.Println("ERROR creating cache key for identity:", id, ", error:", err)
		return nil
	}
	c.Lock()
	defer c.Unlock()
	c.nGets++
	if v, ok := c.cache.Get(key); ok {
		c.nHits++
		c.cache.Put(key, v)
		return v.(ipc.Client)
	}
	return nil
}

func (c *ClientCache) key(id security.PrivateID) (string, error) {
	if id == nil {
		return "", nil
	}

	var buf bytes.Buffer
	b64 := base64.NewEncoder(base64.URLEncoding, &buf)
	if err := vom.NewEncoder(b64).Encode(id.PublicID()); err != nil {
		return "", err
	}
	b64.Close()
	h := md5.New()
	h.Write(buf.Bytes())
	return string(h.Sum(nil)), nil
}

// Stats returns a debug string contain performance stats for the cache.
func (c *ClientCache) Stats() string {
	c.Lock()
	defer c.Unlock()
	hitRate := int64(0)
	if c.nGets > 0 {
		hitRate = c.nHits * 100 / c.nGets
	}
	return fmt.Sprintf("Size:%d Put:%d Get:%d Hits:%d (%d%%) Evicts:%d", c.cache.Size(), c.nPuts, c.nGets, c.nHits, hitRate, c.nEvicts)
}
