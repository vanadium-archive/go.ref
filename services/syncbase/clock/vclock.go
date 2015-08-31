// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"time"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/store"
)

// VClock holds data required to provide an estimate of the UTC time at any
// given point. The fields contained in here are
// - systemTimeAtBoot : the time shown by the system clock at boot.
// - skew             : the difference between the system clock and UTC time.
// - clock            : Instance of clock.SystemClock interface providing access
//                      to the system time.
// - sa               : adapter for storage of clock data.
// - ntpSource        : source for fetching NTP data.
type VClock struct {
	systemTimeAtBoot time.Time
	skew             time.Duration
	clock            SystemClock
	sa               StorageAdapter
	ntpSource        NtpSource
}

func NewVClock(st store.Store) *VClock {
	sysClock := newSystemClock()
	return &VClock{
		clock:     sysClock,
		sa:        NewStorageAdapter(st),
		ntpSource: NewNtpSource(sysClock),
	}
}

// Now returns current UTC time based on the estimation of skew that
// the system clock has with respect to NTP time.
func (c *VClock) Now(ctx *context.T) time.Time {
	clockData := &ClockData{}
	if err := c.sa.GetClockData(ctx, clockData); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			// VClock's cron job to setup UTC time at boot has not been run yet.
			// TODO(jlodhia): uncomment info messages once clock service
			// scheduling is enabled. In absence of clock service, no clock
			// data is present and hence these logs get printed all the time.
			// vlog.Info("No ClockData found while creating a timestamp")
		} else {
			vlog.Errorf("Error while fetching clock data: %v", err)
		}
		// vlog.Info("Returning current system clock time")
		return c.clock.Now()
	}
	skew := time.Duration(clockData.Skew)
	return c.clock.Now().Add(skew)
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
