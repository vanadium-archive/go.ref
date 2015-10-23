// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"strings"
	"sync"

	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"
)

var errVCCacheClosed = reg(".errVCCacheClosed", "vc cache has been closed")

// VCCache implements a set of VIFs keyed by the endpoint of the remote end and the
// local principal. Multiple goroutines can invoke methods on the VCCache simultaneously.
type VCCache struct {
	mu       sync.Mutex
	cache    map[vcKey]*VC  // GUARDED_BY(mu)
	ridCache map[ridKey]*VC // GUARDED_BY(mu)
	started  map[vcKey]bool // GUARDED_BY(mu)
	cond     *sync.Cond
}

// NewVCCache returns a new cache for VCs.
func NewVCCache() *VCCache {
	c := &VCCache{
		cache:    make(map[vcKey]*VC),
		ridCache: make(map[ridKey]*VC),
		started:  make(map[vcKey]bool),
	}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// ReservedFind returns a VC where the remote end of the underlying connection
// is identified by the provided (ep, p.PublicKey). Returns nil if there is no
// such VC.
//
// Iff the cache is closed, ReservedFind will return an error.
// If ReservedFind has no error, the caller is required to call Unreserve, to avoid deadlock.
// The ep, and p.PublicKey in Unreserve must be the same as used in the ReservedFind call.
// During this time, all new ReservedFind calls for this ep and p will Block until
// the corresponding Unreserve call is made.
func (c *VCCache) ReservedFind(ep naming.Endpoint, p security.Principal) (*VC, error) {
	k := c.key(ep, p)
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.started[k] {
		c.cond.Wait()
	}
	if c.cache == nil {
		return nil, verror.New(errVCCacheClosed, nil)
	}
	c.started[k] = true
	if vc, ok := c.ridCache[c.ridkey(ep, p)]; ok {
		return vc, nil
	}
	return c.cache[k], nil
}

// Unreserve marks the status of the ep, p as no longer started, and
// broadcasts waiting threads.
func (c *VCCache) Unreserve(ep naming.Endpoint, p security.Principal) {
	c.mu.Lock()
	delete(c.started, c.key(ep, p))
	c.cond.Broadcast()
	c.mu.Unlock()
}

// Insert adds vc to the cache and returns an error iff the cache has been closed.
func (c *VCCache) Insert(vc *VC) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		return verror.New(errVCCacheClosed, nil)
	}
	ep, principal := vc.RemoteEndpoint(), vc.LocalPrincipal()
	c.cache[c.key(ep, principal)] = vc
	if ep.RoutingID() != naming.NullRoutingID {
		c.ridCache[c.ridkey(ep, principal)] = vc
	}
	return nil
}

// Close marks the VCCache as closed and returns the VCs remaining in the cache.
func (c *VCCache) Close() []*VC {
	c.mu.Lock()
	vcs := make([]*VC, 0, len(c.cache))
	for _, vc := range c.cache {
		vcs = append(vcs, vc)
	}
	c.cache = nil
	c.started = nil
	c.ridCache = nil
	c.mu.Unlock()
	return vcs
}

// Delete removes vc from the cache, returning an error iff the cache has been closed.
func (c *VCCache) Delete(vc *VC) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		return verror.New(errVCCacheClosed, nil)
	}
	ep, principal := vc.RemoteEndpoint(), vc.LocalPrincipal()
	delete(c.cache, c.key(ep, principal))
	delete(c.ridCache, c.ridkey(ep, principal))
	return nil
}

type ridKey struct {
	rid            naming.RoutingID
	localPublicKey string
	blessingNames  string
}

type vcKey struct {
	remoteEP       string
	localPublicKey string // localPublicKey = "" means we are running unencrypted (i.e. SecurityNone)
}

func (c *VCCache) key(ep naming.Endpoint, p security.Principal) vcKey {
	k := vcKey{remoteEP: ep.String()}
	if p != nil {
		k.localPublicKey = p.PublicKey().String()
	}
	return k
}

func (c *VCCache) ridkey(ep naming.Endpoint, p security.Principal) ridKey {
	k := ridKey{rid: ep.RoutingID()}
	if p != nil {
		k.localPublicKey = p.PublicKey().String()
		k.blessingNames = strings.Join(ep.BlessingNames(), ",")
	}
	return k
}
