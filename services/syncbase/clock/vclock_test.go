// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"testing"
)

type systemClockMockImpl struct {
	now int64
}

func (sc *systemClockMockImpl) Now() int64 {
	return sc.now
}

func (sc *systemClockMockImpl) setNow(now int64) {
	sc.now = now
}

var (
	_ SystemClock = (*systemClockImpl)(nil)
)

func TestVClock(t *testing.T) {
	clock := NewVClock()
	sysClock := &systemClockMockImpl{}
	writeTs := int64(4)
	sysClock.setNow(writeTs)
	clock.SetSystemClock(sysClock)

	ts := clock.Now()
	if ts != writeTs {
		t.Errorf("timestamp expected to be %q but found to be %q", writeTs, ts)
	}
}
