// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"net"
	"testing"
	"time"
)

func TestWithMockNtpForErr(t *testing.T) {
	sysClock := MockSystemClock(time.Now(), 0)
	stAdapter := MockStorageAdapter()
	ntpSource := MockNtpSource()
	ntpSource.Err = net.UnknownNetworkError("network err")

	vclock := NewVClockWithMockServices(stAdapter, sysClock, ntpSource)

	if err := vclock.runNtpCheck(nil); err == nil {
		t.Error("Network error expected but not found")
	}

	if stAdapter.clockData != nil {
		t.Error("Non-nil clock data found.")
	}
}

func TestWithMockNtpForDiffBelowThreshold(t *testing.T) {
	sysClock := MockSystemClock(time.Now(), 0) // not used
	stAdapter := MockStorageAdapter()
	originalData := NewClockData(0)
	stAdapter.SetClockData(nil, &originalData)

	ntpSource := MockNtpSource()
	offset := 1800 * time.Millisecond // error threshold is 2 seconds
	ntpSource.Data = &NtpData{offset: offset, delay: 5 * time.Millisecond}

	vclock := NewVClockWithMockServices(stAdapter, sysClock, ntpSource)
	if err := vclock.runNtpCheck(nil); err != nil {
		t.Errorf("Unexpected err: %v", err)
	}
	if isClockDataChanged(stAdapter, &originalData) {
		t.Error("ClockData expected to be unchanged but found updated")
	}
}

func TestWithMockNtpForDiffAboveThreshold(t *testing.T) {
	sysTs := time.Now()
	elapsedTime := 10 * time.Minute
	sysClock := MockSystemClock(sysTs, elapsedTime)

	stAdapter := MockStorageAdapter()
	originalData := NewClockData(0)
	stAdapter.SetClockData(nil, &originalData)

	ntpSource := MockNtpSource()
	skew := 2100 * time.Millisecond // error threshold is 2 seconds
	ntpSource.Data = &NtpData{offset: skew, delay: 5 * time.Millisecond}

	vclock := NewVClockWithMockServices(stAdapter, sysClock, ntpSource)
	if err := vclock.runNtpCheck(nil); err != nil {
		t.Errorf("Unexpected err: %v", err)
	}
	if !isClockDataChanged(stAdapter, &originalData) {
		t.Error("ClockData expected to be updated but found unchanged")
	}
	expectedBootTime := sysTs.Add(-elapsedTime).UnixNano()
	if stAdapter.clockData.Skew != skew.Nanoseconds() {
		t.Errorf("Skew expected to be %d but found %d",
			skew.Nanoseconds(), stAdapter.clockData.Skew)
	}
	if stAdapter.clockData.ElapsedTimeSinceBoot != elapsedTime.Nanoseconds() {
		t.Errorf("ElapsedTime expected to be %d but found %d",
			elapsedTime.Nanoseconds(), stAdapter.clockData.ElapsedTimeSinceBoot)
	}
	if stAdapter.clockData.SystemTimeAtBoot != expectedBootTime {
		t.Errorf("Skew expected to be %d but found %d",
			expectedBootTime, stAdapter.clockData.SystemTimeAtBoot)
	}
}

func TestWithMockNtpForDiffBelowThresholdAndExistingLargeSkew(t *testing.T) {
	sysTs := time.Now()
	elapsedTime := 10 * time.Minute
	sysClock := MockSystemClock(sysTs, elapsedTime)

	stAdapter := MockStorageAdapter()
	originalData := NewClockData(2300 * time.Millisecond.Nanoseconds()) // large skew
	stAdapter.SetClockData(nil, &originalData)

	ntpSource := MockNtpSource()
	skew := 200 * time.Millisecond // error threshold is 2 seconds
	ntpSource.Data = &NtpData{offset: skew, delay: 5 * time.Millisecond}

	vclock := NewVClockWithMockServices(stAdapter, sysClock, ntpSource)
	if err := vclock.runNtpCheck(nil); err != nil {
		t.Errorf("Unexpected err: %v", err)
	}
	if !isClockDataChanged(stAdapter, &originalData) {
		t.Error("ClockData expected to be updated but found unchanged")
	}
	expectedBootTime := sysTs.Add(-elapsedTime).UnixNano()
	if stAdapter.clockData.Skew != skew.Nanoseconds() {
		t.Errorf("Skew expected to be %d but found %d",
			skew.Nanoseconds(), stAdapter.clockData.Skew)
	}
	if stAdapter.clockData.ElapsedTimeSinceBoot != elapsedTime.Nanoseconds() {
		t.Errorf("ElapsedTime expected to be %d but found %d",
			elapsedTime.Nanoseconds(), stAdapter.clockData.ElapsedTimeSinceBoot)
	}
	if stAdapter.clockData.SystemTimeAtBoot != expectedBootTime {
		t.Errorf("Skew expected to be %d but found %d",
			expectedBootTime, stAdapter.clockData.SystemTimeAtBoot)
	}
}

func TestWithMockNtpForDiffBelowThresholdWithNoStoredClockData(t *testing.T) {
	sysTs := time.Now()
	elapsedTime := 10 * time.Minute
	sysClock := MockSystemClock(sysTs, elapsedTime)

	stAdapter := MockStorageAdapter() // no skew data stored

	ntpSource := MockNtpSource()
	skew := 200 * time.Millisecond // error threshold is 2 seconds
	ntpSource.Data = &NtpData{offset: skew, delay: 5 * time.Millisecond}

	vclock := NewVClockWithMockServices(stAdapter, sysClock, ntpSource)
	if err := vclock.runNtpCheck(nil); err != nil {
		t.Errorf("Unexpected err: %v", err)
	}
	if !isClockDataChanged(stAdapter, nil) {
		t.Error("ClockData expected to be updated but found unchanged")
	}
	expectedBootTime := sysTs.Add(-elapsedTime).UnixNano()
	if stAdapter.clockData.Skew != skew.Nanoseconds() {
		t.Errorf("Skew expected to be %d but found %d",
			skew.Nanoseconds(), stAdapter.clockData.Skew)
	}
	if stAdapter.clockData.ElapsedTimeSinceBoot != elapsedTime.Nanoseconds() {
		t.Errorf("ElapsedTime expected to be %d but found %d",
			elapsedTime.Nanoseconds(), stAdapter.clockData.ElapsedTimeSinceBoot)
	}
	if stAdapter.clockData.SystemTimeAtBoot != expectedBootTime {
		t.Errorf("Skew expected to be %d but found %d",
			expectedBootTime, stAdapter.clockData.SystemTimeAtBoot)
	}
}

/*
Following two tests are commented out as they hit the real NTP servers
and can resut into being flaky if the clock of the machine running continuous
test has a skew more than 2 seconds.

func TestWithRealNtp(t *testing.T) {
	stAdapter := MockStorageAdapter()
	originalData := NewClockData(100 * time.Millisecond.Nanoseconds())  // small skew
	stAdapter.SetClockData(nil, &originalData)
	vclock := NewVClockWithMockServices(stAdapter, nil, nil)
	if err := vclock.runNtpCheck(nil); err != nil {
		t.Errorf("Unexpected err: %v", err)
	}
	if isClockDataChanged(stAdapter, &originalData) {
		t.Error("ClockData expected to be unchanged but found updated")
	}
}

func TestWithRealNtpForNoClockData(t *testing.T) {
	stAdapter := MockStorageAdapter()
	vclock := NewVClockWithMockServices(stAdapter, nil, nil)
	if err := vclock.runNtpCheck(nil); err != nil {
		t.Errorf("Unexpected err: %v", err)
	}
	if !isClockDataChanged(stAdapter, nil) {
		t.Error("ClockData expected to be updated but found unchanged")
	}
}
*/

func NewClockData(skew int64) ClockData {
	return ClockData{
		SystemTimeAtBoot:     0,
		Skew:                 skew,
		ElapsedTimeSinceBoot: 0,
	}
}

func isClockDataChanged(stAdapter *storageAdapterMockImpl, originalData *ClockData) bool {
	return stAdapter.clockData != originalData // check for same pointer
}
