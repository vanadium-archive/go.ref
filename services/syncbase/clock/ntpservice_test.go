// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"net"
	"testing"
	"time"

	"v.io/v23/verror"
)

func TestWithMockNtpForErr(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysClock := MockSystemClock(time.Now(), 0)
	ntpSource := MockNtpSource()
	ntpSource.Err = net.UnknownNetworkError("network err")
	vclock := NewVClockWithMockServices(testStore.st, sysClock, ntpSource)

	vclock.runNtpCheck()
	clockData := &ClockData{}
	if err := vclock.GetClockData(vclock.St(), clockData); verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Errorf("Non-nil clock data found: %v", clockData)
	}
}

func TestWithMockNtpForDiffBelowThreshold(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	elapsedTime := time.Duration(50)
	var skew int64 = 0
	sysClock := MockSystemClock(sysTs, elapsedTime)
	originalData := NewClockData(skew)

	ntpSource := MockNtpSource()
	offset := 1800 * time.Millisecond // error threshold is 2 seconds
	ntpSource.Data = &NtpData{
		offset: offset,
		delay:  5 * time.Millisecond,
		ntpTs:  sysTs.Add(offset),
	}

	vclock := NewVClockWithMockServices(testStore.st, sysClock, ntpSource)
	tx := vclock.St().NewTransaction()
	vclock.SetClockData(tx, &originalData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	vclock.runNtpCheck()
	expectedSystemTimeAtBoot := sysTs.UnixNano() - elapsedTime.Nanoseconds()
	expected := newClockData(expectedSystemTimeAtBoot, skew, elapsedTime.Nanoseconds(), &ntpSource.Data.ntpTs, 0, 0)
	VerifyClockData(t, vclock, expected)
}

func TestWithMockNtpForDiffAboveThreshold(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	elapsedTime := 10 * time.Minute
	sysClock := MockSystemClock(sysTs, elapsedTime)
	originalData := NewClockData(0)

	ntpSource := MockNtpSource()
	skew := 2100 * time.Millisecond // error threshold is 2 seconds
	ntpSource.Data = &NtpData{
		offset: skew,
		delay:  5 * time.Millisecond,
		ntpTs:  sysTs.Add(skew),
	}

	vclock := NewVClockWithMockServices(testStore.st, sysClock, ntpSource)
	tx := vclock.St().NewTransaction()
	vclock.SetClockData(tx, &originalData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	vclock.runNtpCheck()
	expectedBootTime := sysTs.Add(-elapsedTime).UnixNano()
	expected := newClockData(expectedBootTime, skew.Nanoseconds(), elapsedTime.Nanoseconds(), &ntpSource.Data.ntpTs, 0, 0)
	VerifyClockData(t, vclock, expected)
}

func TestWithMockNtpForDiffBelowThresholdAndExistingLargeSkew(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	elapsedTime := 10 * time.Minute
	sysClock := MockSystemClock(sysTs, elapsedTime)

	originalData := NewClockData(2300 * time.Millisecond.Nanoseconds()) // large skew

	ntpSource := MockNtpSource()
	skew := 200 * time.Millisecond // error threshold is 2 seconds
	ntpSource.Data = &NtpData{
		offset: skew,
		delay:  5 * time.Millisecond,
		ntpTs:  sysTs.Add(skew),
	}

	vclock := NewVClockWithMockServices(testStore.st, sysClock, ntpSource)
	tx := vclock.St().NewTransaction()
	vclock.SetClockData(tx, &originalData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	vclock.runNtpCheck()
	expectedBootTime := sysTs.Add(-elapsedTime).UnixNano()
	expected := newClockData(expectedBootTime, skew.Nanoseconds(), elapsedTime.Nanoseconds(), &ntpSource.Data.ntpTs, 0, 0)
	VerifyClockData(t, vclock, expected)
}

func TestWithMockNtpForDiffBelowThresholdWithNoStoredClockData(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	sysTs := time.Now()
	elapsedTime := 10 * time.Minute
	sysClock := MockSystemClock(sysTs, elapsedTime)

	ntpSource := MockNtpSource()
	skew := 200 * time.Millisecond // error threshold is 2 seconds
	ntpSource.Data = &NtpData{
		offset: skew,
		delay:  5 * time.Millisecond,
		ntpTs:  sysTs.Add(skew),
	}

	vclock := NewVClockWithMockServices(testStore.st, sysClock, ntpSource)
	// no skew data stored
	vclock.runNtpCheck()
	expectedBootTime := sysTs.Add(-elapsedTime).UnixNano()
	expected := newClockData(expectedBootTime, skew.Nanoseconds(), elapsedTime.Nanoseconds(), &ntpSource.Data.ntpTs, 0, 0)
	VerifyClockData(t, vclock, expected)
}

/*
// Following two tests are commented out as they hit the real NTP servers
// and can resut into being flaky if the clock of the machine running continuous
// test has a skew more than 2 seconds.

func TestWithRealNtp(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	originalData := NewClockData(100 * time.Millisecond.Nanoseconds())  // small skew
	vclock := NewVClockWithMockServices(testStore.st, nil, nil)
	tx := vclock.St().NewTransaction()
	vclock.SetClockData(tx, &originalData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}
	vclock.runNtpCheck()

	clockData := ClockData{}
	if err := vclock.GetClockData(vclock.St(), &clockData); err != nil {
		t.Errorf("error looking up clock data: %v", err)
	}
	fmt.Printf("\nClockData old: %#v, new : %#v", originalData, clockData)
}

func TestWithRealNtpForNoClockData(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)

	vclock := NewVClockWithMockServices(testStore.st, nil, nil)
	vclock.runNtpCheck()

	clockData := ClockData{}
	if err := vclock.GetClockData(vclock.St(), &clockData); err != nil {
		t.Errorf("error looking up clock data: %v", err)
	}
	fmt.Printf("\nClockData: %#v", clockData)
}
*/

func NewClockData(skew int64) ClockData {
	return ClockData{
		SystemTimeAtBoot:     0,
		Skew:                 skew,
		ElapsedTimeSinceBoot: 0,
	}
}
