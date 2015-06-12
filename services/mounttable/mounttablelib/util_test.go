// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mounttablelib

import "time"

// These routines are used by external tests.

// SetServerListClock sets up an alternate clock.
func SetServerListClock(x serverListClock) {
	slc = x
}

// DefaultMaxNodesPerUser returns the maximum number of nodes per user.
func DefaultMaxNodesPerUser() int {
	return defaultMaxNodesPerUser
}

var now = time.Now()

type fakeTime struct {
	theTime time.Time
}

func NewFakeTimeClock() *fakeTime {
	return &fakeTime{theTime: now}
}
func (ft *fakeTime) Now() time.Time {
	return ft.theTime
}
func (ft *fakeTime) Advance(d time.Duration) {
	ft.theTime = ft.theTime.Add(d)
}
