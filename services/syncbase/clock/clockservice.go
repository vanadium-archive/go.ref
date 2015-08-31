// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"math"
	"time"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
)

// This file contains code related to checking current system clock to see
// if it has been changed by any external action.

// runClockCheck estimates the current system time based on saved boottime
// and elapsed time since boot and checks if the system clock shows the same
// time. This involves the following steps:
// 1) Check if system was rebooted since last run. If so update the saved
// ClockData.
// 2) Fetch stored ClockData. If none exists, this is the first time
// runClockCheck has been run. Write new ClockData.
// 3) Estimate current system clock time and check if the actual system clock
// agrees with the estimation. If not update the skew value appropriately.
// 4) Update saved elapsed time since boot. This is used to check if the system
// was rebooted or not. TODO(jlodhia): work with device manager to provide a
// way to notify syncbase if the system was just rebooted.
func (c *VClock) runClockCheck(ctx *context.T) {
	checkSystemRebooted(ctx, c)

	clockData := &ClockData{}
	if err := c.sa.GetClockData(ctx, clockData); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			// VClock's cron job to setup UTC time at boot is being run for the
			// first time. Skew is not known, hence assigning 0.
			writeNewClockData(ctx, c, 0)
		} else {
			vlog.Errorf("Error while fetching clock data: %v", err)
		}
		return
	}

	systemTime := c.clock.Now()
	elapsedTime, err := c.clock.ElapsedTime()
	if err != nil {
		vlog.Errorf("Error while fetching elapsed time: %v", err)
		return
	}

	newClockData := &ClockData{
		SystemTimeAtBoot:     clockData.SystemTimeAtBoot,
		Skew:                 clockData.Skew,
		ElapsedTimeSinceBoot: elapsedTime.Nanoseconds(),
	}

	estimatedClockTime := clockData.SystemBootTime().Add(elapsedTime)
	diff := estimatedClockTime.Sub(systemTime)
	if math.Abs(float64(diff.Nanoseconds())) > util.LocalClockDriftThreshold {
		newClockData.Skew = newClockData.Skew + diff.Nanoseconds()
		newSystemTimeAtBoot := systemTime.Add(-elapsedTime)
		newClockData.SystemTimeAtBoot = newSystemTimeAtBoot.UnixNano()
	}

	if err := c.sa.SetClockData(ctx, newClockData); err != nil {
		vlog.Errorf("Error while setting clock data: %v", err)
	}
}

func writeNewClockData(ctx *context.T, c *VClock, skew time.Duration) {
	systemTime := c.clock.Now()
	elapsedTime, err := c.clock.ElapsedTime()
	if err != nil {
		vlog.Errorf("Error while fetching elapsed time: %v", err)
		return
	}
	systemTimeAtBoot := systemTime.Add(-elapsedTime)
	clockData := &ClockData{
		SystemTimeAtBoot:     systemTimeAtBoot.UnixNano(),
		Skew:                 skew.Nanoseconds(),
		ElapsedTimeSinceBoot: elapsedTime.Nanoseconds(),
	}
	if err := c.sa.SetClockData(ctx, clockData); err != nil {
		vlog.Errorf("Error while setting clock data: %v", err)
	}
}

// checkSystemRebooted compares the elapsed time stored during the last
// run of runClockCheck() to the current elapsed time since boot provided
// by system clock. Since elapsed time is monotonically increasing and cannot
// be changed unless a reboot happens, if the current value is lower than the
// previous value then a reboot has happened since last run. If so, update
// the boot time and elapsed time since boot appropriately.
func checkSystemRebooted(ctx *context.T, c *VClock) bool {
	currentSysTime := c.clock.Now()
	elapsedTime, err := c.clock.ElapsedTime()
	if err != nil {
		vlog.Errorf("Error while fetching elapsed time: %v", err)
		return false
	}

	clockData := &ClockData{}
	if err := c.sa.GetClockData(ctx, clockData); err != nil {
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			vlog.Errorf("Error while fetching clock delta: %v", err)
		}
		// In case of verror.ErrNoExist no clock data present. Nothing needed to
		// be done. writeNewClockData() will write new clock data to storage.
		return false
	}

	if elapsedTime.Nanoseconds() < clockData.ElapsedTimeSinceBoot {
		// Since the elapsed time since last boot provided by the system is
		// less than the elapsed time since boot seen the last time clockservice
		// ran, the system must have rebooted in between.
		clockData.SystemTimeAtBoot = currentSysTime.Add(-elapsedTime).UnixNano()
		clockData.ElapsedTimeSinceBoot = elapsedTime.Nanoseconds()
		if err := c.sa.SetClockData(ctx, clockData); err != nil {
			vlog.Errorf("Error while setting clock data: %v", err)
		}
		return true
	}
	return false
}
