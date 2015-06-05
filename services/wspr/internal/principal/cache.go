// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package principal

import (
	"reflect"
	"sync"
	"time"

	"v.io/v23/security"
)

// BlessingsCacheEntry is an entry in the blessing cache.
type BlessingsCacheEntry struct {
	refCount  uint32 // Keep track of uses so JS knows when to clean up.
	blessings security.Blessings
	dirty     bool // True iff modified since last GC.
}

// BlessingsCache is a cache for blessings.
// This cache is synced with a corresponding cache in Javascript with the goal
// of reducing the number of times that blessings objects are sent in their
// entirety.
// BlessingsCache provides an id for a blessing after a Put() call that is
// understood by Javascript.
// After a new blessings cache is created, recently unused blessings will be
// cleaned up periodically.
type BlessingsCache struct {
	lock       sync.Mutex
	nextId     BlessingsId
	entries    map[BlessingsId]*BlessingsCacheEntry
	notifier   func([]BlessingsCacheMessage)
	inactiveGc bool
}

func NewBlessingsCache(notifier func([]BlessingsCacheMessage), gcPolicy *BlessingsCacheGCPolicy) *BlessingsCache {
	bc := &BlessingsCache{
		entries:  map[BlessingsId]*BlessingsCacheEntry{},
		notifier: notifier,
	}
	go bc.gcLoop(gcPolicy)
	return bc
}

// Put a blessing in the blessing cache and get an id for it that will
// be understood by the Javascript cache.
// If the blessings object is already in the cache, a new entry won't be created
// and the id for the blessings from a previous put will be returned.
func (bc *BlessingsCache) Put(blessings security.Blessings) BlessingsId {
	if blessings.IsZero() {
		return 0
	}

	bc.lock.Lock()
	defer bc.lock.Unlock()

	for id, entry := range bc.entries {
		if reflect.DeepEqual(blessings, entry.blessings) {
			entry.refCount++
			entry.dirty = true
			return id
		}
	}

	bc.nextId++
	id := bc.nextId
	bc.entries[id] = &BlessingsCacheEntry{
		refCount:  1,
		blessings: blessings,
		dirty:     true,
	}
	addMessage := BlessingsCacheAddMessage{
		CacheId:   id,
		Blessings: blessings,
	}
	bc.notifier([]BlessingsCacheMessage{
		BlessingsCacheMessageAdd{
			Value: addMessage,
		},
	})
	return id
}

// BlessingsCacheGCPolicy specifies when GC will occur and provides a function
// to pass notifications of deletions (used to notify Javascript to delete
// the cache items).
type BlessingsCacheGCPolicy struct {
	nextTrigger func() <-chan time.Time
}

// PeriodicGcPolicy periodically performs gc at the specified interval.
func PeriodicGcPolicy(interval time.Duration) *BlessingsCacheGCPolicy {
	return &BlessingsCacheGCPolicy{
		nextTrigger: func() <-chan time.Time { return time.After(interval) },
	}
}

// Stop stops the GC loop and frees up resources.
func (bc *BlessingsCache) Stop() {
	bc.lock.Lock()
	bc.inactiveGc = true
	bc.lock.Unlock()
}

func (bc *BlessingsCache) gcLoop(policy *BlessingsCacheGCPolicy) {
	for {
		bc.lock.Lock()
		if bc.inactiveGc {
			return
		}
		bc.lock.Unlock()
		<-policy.nextTrigger()
		bc.gc(policy)
	}
}

// Perform Garbage Collection.
// All blessings that weren't referenced since the last GC will be removed from
// the cache.
func (bc *BlessingsCache) gc(policy *BlessingsCacheGCPolicy) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	var toDelete []BlessingsCacheMessage
	for id, entry := range bc.entries {
		if !entry.dirty {
			deleteEntry := BlessingsCacheDeleteMessage{
				CacheId:     id,
				DeleteAfter: entry.refCount,
			}
			toDelete = append(toDelete, BlessingsCacheMessageDelete{Value: deleteEntry})
		} else {
			entry.dirty = false
		}
	}

	for _, deleteEntry := range toDelete {
		delete(bc.entries, deleteEntry.(BlessingsCacheMessageDelete).Value.CacheId)
	}

	if len(toDelete) > 0 {
		bc.notifier(toDelete)
	}
}
