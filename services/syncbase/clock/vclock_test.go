// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"testing"
	"time"
)

func TestVClock(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, 0)
	clock := NewVClockWithMockServices(testStore.st, sysClock, nil)

	tx := clock.St().NewTransaction()
	clock.SetClockData(tx, newClockData(0, 0, 0, nil, 0, 0))
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	ts := clock.Now()
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
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	ntpTs := sysTs.Add(time.Duration(-20))
	sysClock := MockSystemClock(sysTs, 0)

	var elapsedTime int64 = 100
	bootTime := sysTs.UnixNano() - elapsedTime
	clockData := newClockData(bootTime, skew, elapsedTime, &ntpTs, 1, 1)
	clock := NewVClockWithMockServices(testStore.st, sysClock, nil)

	tx := clock.St().NewTransaction()
	clock.SetClockData(tx, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	ts := clock.Now()
	if ts == sysTs {
		t.Errorf("timestamp expected to be %q but found to be %q", sysTs, ts)
	}
	if ts.UnixNano() != (sysTs.UnixNano() + skew) {
		t.Errorf("Unexpected vclock timestamp. vclock: %v, sysclock: %v, skew: %v", ts, sysTs, skew)
	}
}

func TestVClockTs(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	vclock := NewVClock(testStore.st)
	skew := time.Minute.Nanoseconds()
	clockData := newClockData(0, skew, 0, nil, 0, 0)

	sysTs := time.Now()
	adjTs := vclock.VClockTs(sysTs, *clockData)

	if adjTs.Sub(sysTs) != time.Minute {
		t.Errorf("Unexpected diff found. SysTs: %v, AdjTs: %v", sysTs, adjTs)
	}
}
