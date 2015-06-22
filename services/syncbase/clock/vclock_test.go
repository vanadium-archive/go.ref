// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"testing"
	"time"
)

type systemClockMockImpl struct {
	now time.Time
}

func (sc *systemClockMockImpl) Now() time.Time {
	return sc.now
}

func (sc *systemClockMockImpl) setNow(now time.Time) {
	sc.now = now
}

var (
	_ SystemClock = (*systemClockImpl)(nil)
)

func TestVClock(t *testing.T) {
	clock := NewVClock()
	sysClock := &systemClockMockImpl{}
	writeTs := time.Now()
	sysClock.setNow(writeTs)
	clock.SetSystemClock(sysClock)

	ts := clock.Now()
	if ts != writeTs {
		t.Errorf("timestamp expected to be %q but found to be %q", writeTs, ts)
	}
}
