// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"sync"
	"time"
)

// Since an idle timer with a short timeout can expire before establishing a VC,
// we provide a fake timer to reduce dependence on real time in unittests.
type fakeTimer struct {
	mu          sync.Mutex
	timeout     time.Duration
	timeoutFunc func()
	timer       timer
	stopped     bool
}

func newFakeTimer(d time.Duration, f func()) *fakeTimer {
	return &fakeTimer{
		timeout:     d,
		timeoutFunc: f,
		timer:       noopTimer{},
	}
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	return t.timer.Stop()
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.timeout = d
	t.stopped = false
	return t.timer.Reset(t.timeout)
}

func (t *fakeTimer) run(release <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	<-release // Wait until notified to run.
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timeout > 0 {
		t.timer = newTimer(t.timeout, t.timeoutFunc)
	}
	if t.stopped {
		t.timer.Stop()
	}
}

// SetFakeTimers causes the idle timers to use a fake timer instead of one
// based on real time. The timers will be triggered when the returned function
// is invoked. (at which point the timer setup will be restored to what it was
// before calling this function)
//
// Usage:
//   triggerTimers := SetFakeTimers()
//   ...
//   triggerTimers()
//
// This function cannot be called concurrently.
func SetFakeTimers() func() {
	backup := newTimer

	var mu sync.Mutex
	var wg sync.WaitGroup
	release := make(chan struct{})
	newTimer = func(d time.Duration, f func()) timer {
		mu.Lock()
		defer mu.Unlock()
		wg.Add(1)
		t := newFakeTimer(d, f)
		go t.run(release, &wg)
		return t
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		newTimer = backup
		close(release)
		wg.Wait()
	}
}
