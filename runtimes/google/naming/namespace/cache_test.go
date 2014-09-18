package namespace

import (
	"fmt"
	"testing"

	"veyron.io/veyron/veyron2/naming"
)

func compatible(server string, servers []mountedServer) bool {
	if len(servers) == 0 {
		return server == ""
	}
	return servers[0].Server == server
}

// TestCache tests the cache directly rather than via the namespace methods.
func TestCache(t *testing.T) {
	preload := []struct {
		name   string
		suffix string
		server string
	}{
		{"/h1//a/b/c/d", "c/d", "/h2"},
		{"/h2//c/d", "d", "/h3"},
		{"/h3//d", "", "/h4:1234"},
	}
	c := newTTLCache()
	for _, p := range preload {
		c.remember(naming.TrimSuffix(p.name, p.suffix), []mountedServer{mountedServer{Server: p.server, TTL: 30}})
	}

	tests := []struct {
		name   string
		suffix string
		server string
	}{
		{"/h1//a/b/c/d", "c/d", "/h2"},
		{"/h2//c/d", "d", "/h3"},
		{"/h3//d", "", "/h4:1234"},
		{"/notintcache", "", ""},
		{"/h1//a/b/f//g", "f//g", "/h2"},
		{"/h3//d//e", "//e", "/h4:1234"},
	}
	for _, p := range tests {
		servers, suffix := c.lookup(p.name)
		if suffix != p.suffix || !compatible(p.server, servers) {
			t.Errorf("%s: unexpected depth: got %v, %s not %s, %s", p.name, servers, suffix, p.server, p.suffix)
		}
	}
}

func TestCacheLimit(t *testing.T) {
	c := newTTLCache().(*ttlCache)
	servers := []mountedServer{mountedServer{Server: "the rain in spain", TTL: 3000}}
	for i := 0; i < maxCacheEntries; i++ {
		c.remember(fmt.Sprintf("%d", i), servers)
		if len(c.entries) > maxCacheEntries {
			t.Errorf("unexpected cache size: got %d not %d", len(c.entries), maxCacheEntries)
		}
	}
	// Adding one more element should reduce us to 3/4 full.
	c.remember(fmt.Sprintf("%d", maxCacheEntries), servers)
	if len(c.entries) != cacheHisteresisSize {
		t.Errorf("cache shrunk wrong amount: got %d not %d", len(c.entries), cacheHisteresisSize)
	}
}

func TestCacheTTL(t *testing.T) {
	c := newTTLCache().(*ttlCache)
	// Fill cache.
	servers := []mountedServer{mountedServer{Server: "the rain in spain", TTL: 3000}}
	for i := 0; i < maxCacheEntries; i++ {
		c.remember(fmt.Sprintf("%d", i), servers)
	}
	// Time out half the entries.
	i := len(c.entries) / 2
	for k := range c.entries {
		c.entries[k][0].TTL = 0
		if i == 0 {
			break
		}
		i--
	}
	// Add an entry and make sure we now have room.
	c.remember(fmt.Sprintf("%d", maxCacheEntries+2), servers)
	if len(c.entries) > cacheHisteresisSize {
		t.Errorf("entries did not timeout: got %d not %d", len(c.entries), cacheHisteresisSize)
	}
}

func TestFlushCacheEntry(t *testing.T) {
	preload := []struct {
		name   string
		server string
	}{
		{"/h1//a/b", "/h2//c"},
		{"/h2//c", "/h3"},
		{"/h3//d", "/h4:1234"},
	}
	ns, _ := New(nil)
	c := ns.resolutionCache.(*ttlCache)
	for _, p := range preload {
		c.remember(p.name, []mountedServer{mountedServer{Server: p.server, TTL: 3000}})
	}
	toflush := "/h1/xyzzy"
	if ns.FlushCacheEntry(toflush) {
		t.Errorf("%s should not have caused anything to flush", toflush)
	}
	toflush = "/h1/a/b/d/e"
	if !ns.FlushCacheEntry(toflush) {
		t.Errorf("%s should have caused something to flush", toflush)
	}
	name := preload[2].name
	if _, ok := c.entries[name]; ok {
		t.Errorf("%s should have been flushed", name)
	}
	if len(c.entries) != 2 {
		t.Errorf("%s flushed too many entries", toflush)
	}
	if !ns.FlushCacheEntry(toflush) {
		t.Errorf("%s should have caused something to flush", toflush)
	}
	name = preload[1].name
	if _, ok := c.entries[name]; ok {
		t.Errorf("%s should have been flushed", name)
	}
	if len(c.entries) != 1 {
		t.Errorf("%s flushed too many entries", toflush)
	}
}

func disabled(ctls []naming.CacheCtl) bool {
	for _, c := range ctls {
		if v, ok := c.(naming.DisableCache); ok && bool(v) {
			return true
		}
	}
	return false
}

func TestCacheDisableEnable(t *testing.T) {
	ns, _ := New(nil)

	// Default should be working resolution cache.
	name := "/h1//a"
	serverName := "/h2//"
	c := ns.resolutionCache.(*ttlCache)
	c.remember(name, []mountedServer{mountedServer{Server: serverName, TTL: 3000}})
	if servers, _ := c.lookup(name); servers == nil || servers[0].Server != serverName {
		t.Errorf("should have found the server in the cache")
	}

	// Turn off the resolution cache.
	ctls := ns.CacheCtl(naming.DisableCache(true))
	if !disabled(ctls) {
		t.Errorf("caching not disabled")
	}
	nc := ns.resolutionCache.(nullCache)
	nc.remember(name, []mountedServer{mountedServer{Server: serverName, TTL: 3000}})
	if servers, _ := nc.lookup(name); servers != nil {
		t.Errorf("should not have found the server in the cache")
	}

	// Turn on the resolution cache.
	ctls = ns.CacheCtl(naming.DisableCache(false))
	if disabled(ctls) {
		t.Errorf("caching disabled")
	}
	c = ns.resolutionCache.(*ttlCache)
	c.remember(name, []mountedServer{mountedServer{Server: serverName, TTL: 3000}})
	if servers, _ := c.lookup(name); servers == nil || servers[0].Server != serverName {
		t.Errorf("should have found the server in the cache")
	}
}
