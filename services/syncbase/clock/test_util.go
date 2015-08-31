// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

// Utilities for testing clock.

import (
	"time"

	"v.io/v23/context"
	"v.io/v23/verror"
)

/////////////////////////////////////////////////
// Mock for StorageAdapter

var _ StorageAdapter = (*storageAdapterMockImpl)(nil)

func MockStorageAdapter() *storageAdapterMockImpl {
	return &storageAdapterMockImpl{}
}

type storageAdapterMockImpl struct {
	clockData *ClockData
	err       error
}

func (sa *storageAdapterMockImpl) GetClockData(ctx *context.T, data *ClockData) error {
	if sa.err != nil {
		return sa.err
	}
	if sa.clockData == nil {
		return verror.NewErrNoExist(ctx)
	}
	*data = *sa.clockData
	return nil
}

func (sa *storageAdapterMockImpl) SetClockData(ctx *context.T, data *ClockData) error {
	if sa.err != nil {
		return sa.err
	}
	sa.clockData = data
	return nil
}

func (sa *storageAdapterMockImpl) SetError(err error) {
	sa.err = err
}

/////////////////////////////////////////////////
// Mock for SystemClock

var _ SystemClock = (*systemClockMockImpl)(nil)

func MockSystemClock(now time.Time, elapsedTime time.Duration) *systemClockMockImpl {
	return &systemClockMockImpl{
		now: now,
		elapsedTime: elapsedTime,
	}
}

type systemClockMockImpl struct {
	now         time.Time
	elapsedTime time.Duration
}

func (sc *systemClockMockImpl) Now() time.Time {
	return sc.now
}

func (sc *systemClockMockImpl) SetNow(now time.Time) {
	sc.now = now
}

func (sc *systemClockMockImpl) ElapsedTime() (time.Duration, error) {
	return sc.elapsedTime, nil
}

func (sc *systemClockMockImpl) SetElapsedTime(elapsed time.Duration) {
	sc.elapsedTime = elapsed
}

/////////////////////////////////////////////////
// Mock for NtpSource

var _ NtpSource = (*ntpSourceMockImpl)(nil)

func MockNtpSource() *ntpSourceMockImpl {
	return &ntpSourceMockImpl{}
}

type ntpSourceMockImpl struct {
	Err  error
	Data *NtpData
}

func (ns *ntpSourceMockImpl) NtpSync(sampleCount int) (*NtpData, error) {
	if ns.Err != nil {
		return nil, ns.Err
	}
	return ns.Data, nil
}

func NewVClockWithMockServices(sa StorageAdapter, sc SystemClock, ns NtpSource) *VClock {
	if sc == nil {
		sc = newSystemClock()
	}
	if ns == nil {
		ns = NewNtpSource(sc)
	}
	return &VClock{
		clock:     sc,
		sa:        sa,
		ntpSource: ns,
	}
}
