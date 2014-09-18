package namespace

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/vlog"
)

// maxCacheEntries is the max number of cache entries to keep.  It exists only so that we
// can avoid edge cases blowing us up the cache.
const maxCacheEntries = 4000

// cacheHisteresisSize is how much we back off to if the cache gets filled up.
const cacheHisteresisSize = (3 * maxCacheEntries) / 4

// cache is a generic interface to the resolution cache.
type cache interface {
	remember(prefix string, servers []mountedServer)
	forget(names []string)
	lookup(name string) ([]mountedServer, string)
}

// ttlCache is an instance of cache that obeys ttl from the mount points.
type ttlCache struct {
	sync.Mutex
	epochStart time.Time
	entries    map[string][]mountedServer
}

// newTTLCache creates an empty ttlCache.
func newTTLCache() cache {
	return &ttlCache{epochStart: time.Now(), entries: make(map[string][]mountedServer)}
}

func isStale(now uint32, servers []mountedServer) bool {
	for _, s := range servers {
		if s.TTL <= now {
			return true
		}
	}
	return false
}

// normalize removes any single trailing slash.  Added for idiots who seem to
// like adding trailing slashes.
func normalize(name string) string {
	if strings.HasSuffix(name, "//") {
		return name
	}
	return strings.TrimSuffix(name, "/")
}

// esecs returns seconds since start of this cache's epoch.
func (c *ttlCache) esecs() uint32 {
	return uint32(time.Since(c.epochStart).Seconds())
}

// randomDrop randomly removes one cache entry.  Assumes we've already locked the cache.
func (c *ttlCache) randomDrop() {
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
func (c *ttlCache) cleaner() {
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
func (c *ttlCache) remember(prefix string, servers []mountedServer) {
	prefix = normalize(prefix)
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
func (c *ttlCache) forget(names []string) {
	c.Lock()
	defer c.Unlock()
	for key := range c.entries {
		for _, n := range names {
			n = normalize(n)
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
func (c *ttlCache) lookup(name string) ([]mountedServer, string) {
	name = normalize(name)
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

// nullCache is an instance of cache that does nothing.
type nullCache int

func newNullCache() cache                                         { return nullCache(1) }
func (nullCache) remember(prefix string, servers []mountedServer) {}
func (nullCache) forget(names []string)                           {}
func (nullCache) lookup(name string) ([]mountedServer, string)    { return nil, "" }
