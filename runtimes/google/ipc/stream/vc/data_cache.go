package vc

import (
	"sync"
)

// dataCache is a thread-safe map for any two types.
type dataCache struct {
	sync.RWMutex
	m map[interface{}]interface{}
}

func newDataCache() *dataCache {
	return &dataCache{m: make(map[interface{}]interface{})}
}

// GetOrInsert first checks if the key exists in the cache with a reader lock.
// If it doesn't exist, it instead acquires a writer lock, creates and stores the new value
// with create and returns value.
func (c *dataCache) GetOrInsert(key interface{}, create func() interface{}) interface{} {
	// We use the read lock for the fastpath. This should be the more common case, so we rarely
	// need a writer lock.
	c.RLock()
	value, exists := c.m[key]
	c.RUnlock()
	if exists {
		return value
	}
	// We acquire the writer lock for the slowpath, and need to re-check if the key exists
	// in the map, since other thread may have snuck in.
	c.Lock()
	defer c.Unlock()
	value, exists = c.m[key]
	if exists {
		return value
	}
	value = create()
	c.m[key] = value
	return value
}
