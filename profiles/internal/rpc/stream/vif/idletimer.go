// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"sync"
	"time"

	"v.io/x/ref/profiles/internal/rpc/stream/id"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
)

// idleTimerMap keeps track of all the flows of each VC and then calls the notify
// function in its own goroutine if there is no flow in a VC for some duration.
type idleTimerMap struct {
	mu         sync.Mutex
	m          map[id.VC]*idleTimer
	notifyFunc func(id.VC)
	stopped    bool
}

type idleTimer struct {
	set     map[id.Flow]struct{}
	timeout time.Duration
	timer   timer
	stopped bool
}

type timer interface {
	// Stop prevents the Timer from firing.
	Stop() bool
	// Reset changes the timer to expire after duration d.
	Reset(d time.Duration) bool
}

// newIdleTimerMap returns a new idle timer map.
func newIdleTimerMap(f func(id.VC)) *idleTimerMap {
	return &idleTimerMap{
		m:          make(map[id.VC]*idleTimer),
		notifyFunc: f,
	}
}

// Stop stops idle timers for all VC.
func (m *idleTimerMap) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	for _, t := range m.m {
		if !t.stopped {
			t.timer.Stop()
			t.stopped = true
		}
	}
	m.stopped = true
}

// Insert starts the idle timer for the given VC. If there is no active flows
// in the VC for the duration d, the notify function will be called in its own
// goroutine. If d is zero, the idle timer is disabled.
func (m *idleTimerMap) Insert(vci id.VC, d time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return false
	}
	if _, exists := m.m[vci]; exists {
		return false
	}
	t := &idleTimer{
		set:     make(map[id.Flow]struct{}),
		timeout: d,
	}
	if t.timeout > 0 {
		t.timer = newTimer(t.timeout, func() { m.notifyFunc(vci) })
	} else {
		t.timer = noopTimer{}
	}
	m.m[vci] = t
	return true
}

// Delete deletes the idle timer for the given VC.
func (m *idleTimerMap) Delete(vci id.VC) {
	m.mu.Lock()
	if t, exists := m.m[vci]; exists {
		if !t.stopped {
			t.timer.Stop()
		}
		delete(m.m, vci)
	}
	m.mu.Unlock()
}

// InsertFlow inserts the given flow to the given VC. All system flows will be ignored.
func (m *idleTimerMap) InsertFlow(vci id.VC, fid id.Flow) {
	if fid < vc.NumReservedFlows {
		return
	}
	m.mu.Lock()
	if t, exists := m.m[vci]; exists {
		t.set[fid] = struct{}{}
		if !t.stopped {
			t.timer.Stop()
			t.stopped = true
		}
	}
	m.mu.Unlock()
}

// DeleteFlow deletes the given flow from the VC vci.
func (m *idleTimerMap) DeleteFlow(vci id.VC, fid id.Flow) {
	m.mu.Lock()
	if t, exists := m.m[vci]; exists {
		delete(t.set, fid)
		if len(t.set) == 0 && t.stopped && !m.stopped {
			t.timer.Reset(t.timeout)
			t.stopped = false
		}
	}
	m.mu.Unlock()
}

// To avoid dependence on real times in unittests, the factory function for timers
// can be overridden (with SetFakeTimers). This factory function should only be
// overridden for unittests.
var newTimer = defaultTimerFactory

func defaultTimerFactory(d time.Duration, f func()) timer { return time.AfterFunc(d, f) }

// A noop timer.
type noopTimer struct{}

func (t noopTimer) Stop() bool                 { return false }
func (t noopTimer) Reset(d time.Duration) bool { return false }
