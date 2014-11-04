package namespace

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"veyron.io/veyron/veyron2/naming"
	verror "veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"
)

// maxCacheEntries is the max number of cache entries to keep.  It exists only so that we
// can avoid edge cases blowing us up the cache.
const maxCacheEntries = 4000

// cacheHisteresisSize is how much we back off to if the cache gets filled up.
const cacheHisteresisSize = (3 * maxCacheEntries) / 4

// cache is a generic interface to the resolution cache.
type cache interface {
	remember(prefix string, entry *naming.MountEntry)
	forget(names []string)
	lookup(name string) (naming.MountEntry, error)
}

// ttlCache is an instance of cache that obeys ttl from the mount points.
type ttlCache struct {
	sync.Mutex
	entries map[string]naming.MountEntry
}

// newTTLCache creates an empty ttlCache.
func newTTLCache() cache {
	return &ttlCache{entries: make(map[string]naming.MountEntry)}
}

func isStale(now time.Time, e naming.MountEntry) bool {
	for _, s := range e.Servers {
		if s.Expires.Before(now) {
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
	now := time.Now()
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
func (c *ttlCache) remember(prefix string, entry *naming.MountEntry) {
	// Remove suffix.  We only care about the name that gets us
	// to the mounttable from the last mounttable.
	prefix = normalize(prefix)
	prefix = naming.TrimSuffix(prefix, entry.Name)
	// Copy the entry.
	var ce naming.MountEntry
	for _, s := range entry.Servers {
		ce.Servers = append(ce.Servers, s)
	}
	ce.MT = entry.MT
	// All keys must be terminal.
	prefix = naming.MakeTerminal(prefix)
	c.Lock()
	// Enforce an upper limit on the cache size.
	if len(c.entries) >= maxCacheEntries {
		if _, ok := c.entries[prefix]; !ok {
			c.cleaner()
		}
	}
	c.entries[prefix] = ce
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
// prefix, and suffix.  If any of the associated servers is expired, don't return anything
// since that would reduce availability.
func (c *ttlCache) lookup(name string) (naming.MountEntry, error) {
	name = normalize(name)
	c.Lock()
	defer c.Unlock()
	now := time.Now()
	for prefix, suffix := name, ""; len(prefix) > 0; prefix, suffix = backup(prefix, suffix) {
		e, ok := c.entries[prefix]
		if !ok {
			continue
		}
		if isStale(now, e) {
			return e, verror.Make(naming.ErrNoSuchName, nil, name)
		}
		vlog.VI(2).Infof("namespace cache %s -> %v %s", name, e.Servers, e.Name)
		e.Name = suffix
		return e, nil
	}
	return naming.MountEntry{}, verror.Make(naming.ErrNoSuchName, nil, name)
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

func newNullCache() cache                                          { return nullCache(1) }
func (nullCache) remember(prefix string, entry *naming.MountEntry) {}
func (nullCache) forget(names []string)                            {}
func (nullCache) lookup(name string) (e naming.MountEntry, err error) {
	return e, verror.Make(naming.ErrNoSuchName, nil, name)
}
