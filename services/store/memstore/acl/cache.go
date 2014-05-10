package acl

import (
	"bytes"

	"veyron/runtimes/google/lib/functional"
	"veyron/runtimes/google/lib/functional/rb"
	"veyron2/storage"
)

// FindFunc is used to fetch the ACL given its ID.
type FindFunc func(id storage.ID) *storage.ACL

// Cache keeps a map from ID -> ACL, based on the contents of the store.
//
// Cache is thread-compatible -- mutating methods require the caller to
// have exclusive access to the object.
//
// TODO(jyh): Currently, the cache can grow without bound.  Implement a
// replacement policy, for a cache with a bounded number of entries.
type Cache struct {
	contents functional.Set // *cacheEntry
	find     FindFunc
}

// cacheEntry is an entry in the cache, mapping storage.ID -> storage.ACL.
type cacheEntry struct {
	id  storage.ID
	acl *storage.ACL
}

var (
	emptyCacheContents = rb.NewSet(compareCacheEntriesByID)
)

// compareCacheEntriesByID compares the two cells' IDs.
func compareCacheEntriesByID(a, b interface{}) bool {
	return bytes.Compare(a.(*cacheEntry).id[:], b.(*cacheEntry).id[:]) < 0
}

// NewCache returns an empty cache.
func NewCache(find FindFunc) Cache {
	return Cache{contents: emptyCacheContents, find: find}
}

// Clear resets the cache.
func (c *Cache) Clear() {
	c.contents = emptyCacheContents
	c.find = nil
}

// Invalidate removes cache entry.  It should be called whenever the ACL
// associated with the ID is changed.
func (c *Cache) Invalidate(id storage.ID) {
	c.contents = c.contents.Remove(&cacheEntry{id: id})
}

// UpdateFind changes the find function.
func (c *Cache) UpdateFinder(find FindFunc) {
	c.find = find
}

// get fetches a cache entry.  If the entry is not in the cache, the function
// <find> is used to fetch the value, the result is saved in the cache, and
// returned.
func (c *Cache) get(id storage.ID) *storage.ACL {
	e := &cacheEntry{id: id}
	x, ok := c.contents.Get(e)
	if ok {
		return x.(*cacheEntry).acl
	}
	acl := c.find(id)
	if acl == nil {
		return nil
	}
	e.acl = acl
	c.contents = c.contents.Put(e)
	return acl
}
