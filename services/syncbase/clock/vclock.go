// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"time"

	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// VClock holds data required to provide an estimate of the UTC time at any
// given point. The fields contained in here are
// - SysClock         : Instance of clock.SystemClock interface providing access
//                      to the system time.
// - ntpSource        : source for fetching NTP data.
// - st               : store object representing syncbase (not a specific db).
type VClock struct {
	SysClock  SystemClock
	ntpSource NtpSource
	st        store.Store
}

func NewVClock(st store.Store) *VClock {
	sysClock := newSystemClock()
	return &VClock{
		SysClock:  sysClock,
		ntpSource: NewNtpSource(sysClock),
		st:        st,
	}
}

// Now returns current UTC time based on the estimation of skew that
// the system clock has with respect to NTP time.
// The timestamp is based on system timestamp taken after database lookup
// for skew.
func (c *VClock) Now() time.Time {
	clockData := &ClockData{}
	if err := c.GetClockData(c.st, clockData); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			// VClock's cron job to setup UTC time at boot has not been run yet.
			vlog.VI(2).Info("clock: No ClockData found while creating a timestamp")
		} else {
			vlog.Errorf("clock: Error while fetching clock data: %v", err)
		}
		vlog.VI(2).Info("clock: Returning current system clock time")
		return c.SysClock.Now()
	}
	skew := time.Duration(clockData.Skew)
	return c.SysClock.Now().Add(skew)
}

// NowNoLookup is similar to Now() but does not do a database lookup to get
// the ClockData. Useful when clockData is already being fetched by calling
// code.
func (c *VClock) NowNoLookup(clockData ClockData) time.Time {
	skew := time.Duration(clockData.Skew)
	return c.SysClock.Now().Add(skew)
}

// VClockTs converts any system timestamp to a syncbase timestamp based on
// skew provided in ClockData. Useful when the caller does not want the
// time taken to fetch ClockData to be reflected in a timestamp.
// example usage:
// RpcMethod(req) {
//     recvSysTs := vclock.SysClock.Now()
//     clockData := vclock.GetClockData()
//     recvTs := vclock.VClockTs(recvSysTs, clockData)
//     ...
// }
func (c *VClock) VClockTs(sysTime time.Time, clockData ClockData) time.Time {
	return sysTime.Add(time.Duration(clockData.Skew))
}

func (c *VClock) St() store.Store {
	return c.st
}

func (c *VClock) GetClockData(st store.StoreReader, data *ClockData) error {
	return util.Get(nil, st, clockDataKey(), data)
}

func (c *VClock) SetClockData(tx store.Transaction, data *ClockData) error {
	return util.Put(nil, tx, clockDataKey(), data)
}

func clockDataKey() string {
	return util.ClockPrefix
}

///////////////////////////////////////////////////
// Implementation for SystemClock.

type systemClockImpl struct{}

// Returns system time in UTC.
func (sc *systemClockImpl) Now() time.Time {
	return time.Now().UTC()
}

var _ SystemClock = (*systemClockImpl)(nil)

func newSystemClock() SystemClock {
	return &systemClockImpl{}
}
