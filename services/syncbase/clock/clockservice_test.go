// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"testing"
	"time"
)

const (
	constElapsedTime int64 = 50
)

func TestWriteNewClockData(t *testing.T) {
	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	stAdapter := MockStorageAdapter()

	clock := NewVClockWithMockServices(stAdapter, sysClock, nil)

	writeNewClockData(nil, clock, 0)

	expectedSystemTimeAtBoot := sysTs.UnixNano() - constElapsedTime
	verifyClockData(t, stAdapter, 0, expectedSystemTimeAtBoot, constElapsedTime)
}

// This test runs the following scenarios
// 1) Run checkSystemRebooted() with no ClockData stored
// Result: no op.
// 2) Run checkSystemRebooted() with ClockData that has SystemTimeAtBoot higher
// than the current elapsed time.
// Result: A new ClockData is written with updated SystemTimeAtBoot and
// elapsed time.
// 3) Run checkSystemRebooted() again after moving the sysClock forward
// Result: no op.
func TestCheckSystemRebooted(t *testing.T) {
	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	stAdapter := MockStorageAdapter()

	clock := NewVClockWithMockServices(stAdapter, sysClock, nil)

	// stAdapter will return ErrNoExist while fetching ClockData
	// checkSystemRebooted should return false.
	if checkSystemRebooted(nil, clock) {
		t.Error("Unexpected return value")
	}

	// Set clock data with elapsed time greater than constElapsedTime
	clockData := &ClockData{25003, 25, 34569}
	stAdapter.SetClockData(nil, clockData)

	if !checkSystemRebooted(nil, clock) {
		t.Error("Unexpected return value")
	}
	expectedSystemTimeAtBoot := sysTs.UnixNano() - constElapsedTime
	verifyClockData(t, stAdapter, 25, expectedSystemTimeAtBoot, constElapsedTime)

	// move clock forward without reboot and run checkSystemRebooted again
	var timePassed int64 = 200
	newSysTs := sysTs.Add(time.Duration(timePassed))
	sysClock.SetNow(newSysTs)
	sysClock.SetElapsedTime(time.Duration(constElapsedTime + timePassed))

	if checkSystemRebooted(nil, clock) {
		t.Error("Unexpected return value")
	}
	expectedSystemTimeAtBoot = sysTs.UnixNano() - constElapsedTime
	verifyClockData(t, stAdapter, 25, expectedSystemTimeAtBoot, constElapsedTime)
}

// Setup: No prior ClockData present.
// Result: A new ClockData value gets set.
func TestRunClockCheck1(t *testing.T) {
	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	stAdapter := MockStorageAdapter()

	clock := NewVClockWithMockServices(stAdapter, sysClock, nil)

	clock.runClockCheck(nil)
	expectedSystemTimeAtBoot := sysTs.UnixNano() - constElapsedTime
	verifyClockData(t, stAdapter, 0, expectedSystemTimeAtBoot, constElapsedTime)
}

// Setup: ClockData present, system clock elapsed time is lower than whats
// stored in clock data.
// Result: A new ClockData value gets set with new system boot time and elapsed
// time, skew remains the same.
func TestRunClockCheck2(t *testing.T) {
	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	stAdapter := MockStorageAdapter()
	// Set clock data with elapsed time greater than constElapsedTime
	clockData := &ClockData{25003, 25, 34569}
	stAdapter.SetClockData(nil, clockData)

	clock := NewVClockWithMockServices(stAdapter, sysClock, nil)

	clock.runClockCheck(nil)
	expectedSystemTimeAtBoot := sysTs.UnixNano() - constElapsedTime
	verifyClockData(t, stAdapter, 25, expectedSystemTimeAtBoot, constElapsedTime)
}

// Setup: ClockData present, system clock gets a skew of 10 seconds
// Result: A new ClockData value gets set with new elapsed time and skew,
// system boot time remains the same.
func TestRunClockCheck3(t *testing.T) {
	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	stAdapter := MockStorageAdapter()

	bootTs := sysTs.Add(time.Duration(-constElapsedTime))
	oldSkew := 25 * time.Second
	clockData := &ClockData{bootTs.UnixNano(), oldSkew.Nanoseconds(), 40}
	stAdapter.SetClockData(nil, clockData)

	clock := NewVClockWithMockServices(stAdapter, sysClock, nil)

	// introduce a change in sys clock
	extraSkew := 10 * time.Second // moves clock closer to UTC
	changedSysTs := sysTs.Add(extraSkew)
	sysClock.SetNow(changedSysTs)
	newSkew := 15 * time.Second

	clock.runClockCheck(nil)
	expectedSystemTimeAtBoot := bootTs.UnixNano() + extraSkew.Nanoseconds()
	verifyClockData(t, stAdapter, newSkew.Nanoseconds(), expectedSystemTimeAtBoot, constElapsedTime)
}

func TestWithRealSysClock(t *testing.T) {
	stAdapter := MockStorageAdapter()
	clock := NewVClockWithMockServices(stAdapter, nil, nil)

	writeNewClockData(nil, clock, 0)

	// Verify if clock data was written to StorageAdapter
	clockData := &ClockData{}
	if err := stAdapter.GetClockData(nil, clockData); err != nil {
		t.Errorf("Expected to find clockData, received error: %v", err)
	}

	// Verify that calling checkSystemRebooted() does nothing
	if checkSystemRebooted(nil, clock) {
		t.Error("Unexpected return value")
	}

	// sleep for 1 second more than the skew threshold
	time.Sleep(1800 * time.Millisecond)

	// Verify that calling runClockCheck() only updates elapsed time
	clock.runClockCheck(nil)
	newClockData := &ClockData{}
	if err := stAdapter.GetClockData(nil, newClockData); err != nil {
		t.Errorf("Expected to find clockData, received error: %v", err)
	}
	if newClockData.Skew != clockData.Skew {
		t.Errorf("Unexpected value for skew: %d", newClockData.Skew)
	}
	if newClockData.ElapsedTimeSinceBoot <= clockData.ElapsedTimeSinceBoot {
		t.Errorf("Unexpected value for elapsed time: %d",
			newClockData.ElapsedTimeSinceBoot)
	}
	if newClockData.SystemTimeAtBoot != clockData.SystemTimeAtBoot {
		t.Errorf("SystemTimeAtBoot expected: %d, found: %d",
			clockData.SystemTimeAtBoot, newClockData.SystemTimeAtBoot)
	}
}

func verifyClockData(t *testing.T, stAdapter StorageAdapter, skew int64,
	sysTimeAtBoot int64, elapsedTime int64) {
	// verify ClockData
	clockData := &ClockData{}
	if err := stAdapter.GetClockData(nil, clockData); err != nil {
		t.Errorf("Expected to find clockData, found error: %v", err)
	}

	if clockData.Skew != skew {
		t.Errorf("Expected value for skew: %d, found: %d", skew, clockData.Skew)
	}
	if clockData.ElapsedTimeSinceBoot != elapsedTime {
		t.Errorf("Expected value for elapsed time: %d, found: %d", elapsedTime,
			clockData.ElapsedTimeSinceBoot)
	}
	if clockData.SystemTimeAtBoot != sysTimeAtBoot {
		t.Errorf("Expected value for SystemTimeAtBoot: %d, found: %d",
			sysTimeAtBoot, clockData.SystemTimeAtBoot)
	}
}
