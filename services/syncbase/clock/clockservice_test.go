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
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))

	clock := NewVClockWithMockServices(testStore.st, sysClock, nil)

	tx := clock.St().NewTransaction()
	writeNewClockData(tx, clock, 0, nil)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	expectedSystemTimeAtBoot := sysTs.UnixNano() - constElapsedTime
	expected := newClockData(expectedSystemTimeAtBoot, 0, constElapsedTime, nil, 0, 0)
	VerifyClockData(t, clock, expected)
}

// This test runs the following scenarios
// 1) Run checkSystemRebooted() with ClockData that has SystemTimeAtBoot higher
// than the current elapsed time.
// Result: A new ClockData is written with updated SystemTimeAtBoot and
// elapsed time.
// 2) Run checkSystemRebooted() again after moving the sysClock forward
// Result: no op.
func TestCheckSystemRebooted(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	ntpTs := sysTs.Add(time.Duration(-10))
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	clock := NewVClockWithMockServices(testStore.st, sysClock, nil)

	// Set clock data with elapsed time greater than constElapsedTime
	clockData := newClockData(25003, 25, 34569, &ntpTs, 1, 1)
	tx := clock.St().NewTransaction()
	clock.SetClockData(tx, clockData)
	if !checkSystemRebooted(tx, clock, clockData) {
		t.Error("Unexpected return value")
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	expectedSystemTimeAtBoot := sysTs.UnixNano() - constElapsedTime
	expected := newClockData(expectedSystemTimeAtBoot, 25, constElapsedTime, &ntpTs, 2 /*NumReboots incremented*/, 1)
	VerifyClockData(t, clock, expected)

	// move clock forward without reboot and run checkSystemRebooted again
	var timePassed int64 = 200
	newSysTs := sysTs.Add(time.Duration(timePassed))
	sysClock.SetNow(newSysTs)
	sysClock.SetElapsedTime(time.Duration(constElapsedTime + timePassed))

	tx = clock.St().NewTransaction()
	if checkSystemRebooted(tx, clock, clockData) {
		t.Error("Unexpected return value")
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	expectedSystemTimeAtBoot = sysTs.UnixNano() - constElapsedTime
	expected = newClockData(expectedSystemTimeAtBoot, 25, constElapsedTime, &ntpTs, 2, 1)
	VerifyClockData(t, clock, expected)
}

// Setup: No prior ClockData present.
// Result: A new ClockData value gets set.
func TestRunClockCheck1(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	clock := NewVClockWithMockServices(testStore.st, sysClock, nil)

	clock.runClockCheck()
	expectedSystemTimeAtBoot := sysTs.UnixNano() - constElapsedTime
	expected := newClockData(expectedSystemTimeAtBoot, 0, constElapsedTime, nil, 0, 0)
	VerifyClockData(t, clock, expected)
}

// Setup: ClockData present, system clock elapsed time is lower than whats
// stored in clock data.
// Result: A new ClockData value gets set with new system boot time and elapsed
// time, skew remains the same.
func TestRunClockCheck2(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	ntpTs := sysTs.Add(time.Duration(-10))
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	clock := NewVClockWithMockServices(testStore.st, sysClock, nil)

	// Set clock data with elapsed time greater than constElapsedTime
	clockData := newClockData(25003, 25, 34569, &ntpTs, 1, 1)
	tx := clock.St().NewTransaction()
	clock.SetClockData(tx, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	clock.runClockCheck()
	expectedSystemTimeAtBoot := sysTs.UnixNano() - constElapsedTime
	expected := newClockData(expectedSystemTimeAtBoot, 25, constElapsedTime, &ntpTs, 2 /*NumReboots incremented*/, 1)
	VerifyClockData(t, clock, expected)
}

// Setup: ClockData present, system clock gets a skew of 10 seconds
// Result: A new ClockData value gets set with new elapsed time and skew,
// system boot time remains the same.
func TestRunClockCheck3(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	ntpTs := sysTs.Add(time.Duration(-20))
	sysClock := MockSystemClock(sysTs, time.Duration(constElapsedTime))
	clock := NewVClockWithMockServices(testStore.st, sysClock, nil)

	bootTs := sysTs.Add(time.Duration(-constElapsedTime))
	oldSkew := 25 * time.Second
	clockData := newClockData(bootTs.UnixNano(), oldSkew.Nanoseconds(), 40, &ntpTs, 1, 1)

	tx := clock.St().NewTransaction()
	clock.SetClockData(tx, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	// introduce a change in sys clock
	extraSkew := 10 * time.Second // moves clock closer to UTC
	changedSysTs := sysTs.Add(extraSkew)
	sysClock.SetNow(changedSysTs)
	newSkew := 15 * time.Second

	clock.runClockCheck()
	expectedSystemTimeAtBoot := bootTs.UnixNano() + extraSkew.Nanoseconds()
	expected := newClockData(expectedSystemTimeAtBoot, newSkew.Nanoseconds(), constElapsedTime, &ntpTs, 1, 1)
	VerifyClockData(t, clock, expected)
}

func TestWithRealSysClock(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	clock := NewVClockWithMockServices(testStore.st, nil, nil)
	ntpTs := time.Now().Add(time.Duration(-5000))

	tx := clock.St().NewTransaction()
	writeNewClockData(tx, clock, 0, &ntpTs)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	// Verify if clock data was written to StorageAdapter
	clockData := &ClockData{}
	if err := clock.GetClockData(clock.St(), clockData); err != nil {
		t.Errorf("Expected to find clockData, received error: %v", err)
	}

	// Verify that calling checkSystemRebooted() does nothing
	tx = clock.St().NewTransaction()
	if checkSystemRebooted(tx, clock, clockData) {
		t.Error("Unexpected return value")
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	// sleep for 1 second more than the skew threshold
	time.Sleep(1800 * time.Millisecond)

	// Verify that calling runClockCheck() only updates elapsed time
	clock.runClockCheck()
	newClockData := &ClockData{}
	if err := clock.GetClockData(clock.St(), newClockData); err != nil {
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
	if !timeEquals(clockData.LastNtpTs, newClockData.LastNtpTs) {
		t.Errorf("Expected value for LastNtpTs: %v, found: %v",
			fmtTime(clockData.LastNtpTs), fmtTime(newClockData.LastNtpTs))
	}
}
