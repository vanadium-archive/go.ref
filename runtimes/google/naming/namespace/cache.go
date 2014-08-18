package namespace

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"veyron2/naming"
	"veyron2/vlog"
)

// maxCacheEntries is the max number of cache entries to keep.  It exists only so that we
// can avoid edge cases blowing us up the cache.
const maxCacheEntries = 4000

// cacheHisteresisSize is how much we back off to if the cache gets filled up.
const cacheHisteresisSize = (3 * maxCacheEntries) / 4

type cache struct {
	sync.Mutex
	epochStart time.Time
	entries    map[string][]mountedServer
}

// newCache creates a empty cache.
func newCache() *cache {
	return &cache{epochStart: time.Now(), entries: make(map[string][]mountedServer)}
}

func isStale(now uint32, servers []mountedServer) bool {
	for _, s := range servers {
		if s.TTL <= now {
			return true
		}
	}
	return false
}

// esecs returns seconds since start of this cache's epoch.
func (c *cache) esecs() uint32 {
	return uint32(time.Since(c.epochStart).Seconds())
}

// randomDrop randomly removes one cache entry.  Assumes we've already locked the cache.
func (c *cache) randomDrop() {
	n := rand.Intn(len(c.entries))
	for k := range c.entries {
		if n == 0 {
			delete(c.entries, k)
			break
		}
		n--
	}
}

// cleaner reduces the number of entries.  Assumes we've already locked the cache.
func (c *cache) cleaner() {
	// First dump any stale entries.
	now := c.esecs()
	for k, v := range c.entries {
		if len(c.entries) < cacheHisteresisSize {
			return
		}
		if isStale(now, v) {
			delete(c.entries, k)
		}
	}

	// If we haven't gotten low enough, dump randomly.
	for len(c.entries) >= cacheHisteresisSize {
		c.randomDrop()
	}
}

// remember the servers associated with name with suffix removed.
func (c *cache) remember(prefix string, servers []mountedServer) {
	for i := range servers {
		// Remember when this cached entry times out relative to our epoch.
		servers[i].TTL += c.esecs()
	}
	c.Lock()
	// Enforce an upper limit on the cache size.
	if len(c.entries) >= maxCacheEntries {
		if _, ok := c.entries[prefix]; !ok {
			c.cleaner()
		}
	}
	c.entries[prefix] = servers
	c.Unlock()
}

// forget cache entries whose index begins with an element of names.  If names is nil
// forget all cached entries.
func (c *cache) forget(names []string) {
	c.Lock()
	defer c.Unlock()
	for key := range c.entries {
		for _, n := range names {
			if strings.HasPrefix(key, n) {
				delete(c.entries, key)
				break
			}
		}
	}
}

// lookup searches the cache for a maximal prefix of name and returns the associated servers,
// prefix, and suffix.  If any of the associated servers is past its TTL, don't return anything
// since that would reduce availability.
func (c *cache) lookup(name string) ([]mountedServer, string) {
	c.Lock()
	defer c.Unlock()
	now := c.esecs()
	for prefix, suffix := name, ""; len(prefix) > 0; prefix, suffix = backup(prefix, suffix) {
		servers, ok := c.entries[prefix]
		if !ok {
			continue
		}
		if isStale(now, servers) {
			return nil, ""
		}
		vlog.VI(2).Infof("namespace cache %s -> %v %s", name, servers, suffix)
		return servers, suffix
	}
	return nil, ""
}

// backup moves the last element of the prefix to the suffix.  "//" is preserved.  Thus
//   a/b//c, -> a/b, //c
//   /a/b,c/d -> /a, b/c/d
//   /a,b/c/d -> ,/a/b/c/d
func backup(prefix, suffix string) (string, string) {
	for i := len(prefix) - 1; i > 0; i-- {
		if prefix[i] != '/' {
			continue
		}
		if prefix[i-1] == '/' {
			suffix = naming.Join(prefix[i-1:], suffix)
			prefix = prefix[:i-1]
		} else {
			suffix = naming.Join(prefix[i+1:], suffix)
			prefix = prefix[:i]
		}
		return prefix, suffix
	}
	return "", naming.Join(prefix, suffix)
}
