// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"testing"
	"time"

	"v.io/v23/verror"
)

func TestVClock(t *testing.T) {
	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, 0)
	stAdapter := MockStorageAdapter()
	stAdapter.SetClockData(nil, &ClockData{0, 0, 0})
	clock := NewVClockWithMockServices(stAdapter, sysClock, nil)

	ts := clock.Now(nil)
	if ts != sysTs {
		t.Errorf("timestamp expected to be %q but found to be %q", sysTs, ts)
	}
}

func TestVClockWithSkew(t *testing.T) {
	// test with positive skew
	checkSkew(t, 5)
	// test with negative skew
	checkSkew(t, -5)
}

func checkSkew(t *testing.T, skew int64) {
	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, 0)

	var elapsedTime int64 = 100
	stAdapter := MockStorageAdapter()
	bootTime := sysTs.UnixNano() - elapsedTime
	clockData := ClockData{bootTime, skew, elapsedTime}
	stAdapter.SetClockData(nil, &clockData)

	clock := NewVClockWithMockServices(stAdapter, sysClock, nil)

	ts := clock.Now(nil)
	if ts == sysTs {
		t.Errorf("timestamp expected to be %q but found to be %q", sysTs, ts)
	}
	if ts.UnixNano() != (sysTs.UnixNano() + skew) {
		t.Errorf("Unexpected vclock timestamp. vclock: %v, sysclock: %v, skew: %v", ts, sysTs, skew)
	}
}

func TestVClockWithInternalErr(t *testing.T) {
	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, 0)

	stAdapter := MockStorageAdapter()
	stAdapter.SetError(verror.NewErrInternal(nil))

	clock := NewVClockWithMockServices(stAdapter, sysClock, nil)

	// Internal err should result in vclock falling back to the system clock.
	ts := clock.Now(nil)
	if ts != sysTs {
		t.Errorf("timestamp expected to be %q but found to be %q", sysTs, ts)
	}
}
