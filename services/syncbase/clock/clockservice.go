// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

// This file contains code related to checking current system clock to see
// if it has been changed by any external action.
import (
	"math"
	"time"

	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// runClockCheck estimates the current system time based on saved boottime
// and elapsed time since boot and checks if the system clock shows the same
// time. This involves the following steps:
// 1) Fetch stored ClockData. If none exists, this is the first time
// runClockCheck has been run. Write new ClockData and exit.
// 2) Check if system was rebooted since last run. If so update the saved
// ClockData.
// 3) Estimate current system clock time and check if the actual system clock
// agrees with the estimation. If not update the skew value appropriately.
// 4) Update saved elapsed time since boot. This is used to check if the system
// was rebooted or not. TODO(jlodhia): work with device manager to provide a
// way to notify syncbase if the system was just rebooted.
func (c *VClock) runClockCheck() {
	vlog.VI(2).Info("clock: runClockCheck: starting clock check")
	defer vlog.VI(2).Info("clock: runClockCheck: clock check finished")

	err := store.RunInTransaction(c.st, func(tx store.Transaction) error {
		return c.runClockCheckInternal(tx)
	})
	if err != nil {
		vlog.Errorf("clock: runClockCheck: error while commiting: %v", err)
	}
}

func (c *VClock) runClockCheckInternal(tx store.Transaction) error {
	clockData := &ClockData{}
	if err := c.GetClockData(tx, clockData); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			// VClock's cron job to setup UTC time at boot is being run for the
			// first time. Skew is not known, hence assigning 0.
			vlog.VI(2).Info("clock: runClockCheck: no clock data found, writing new data")
			writeNewClockData(tx, c, 0, nil)
			return nil
		}
		vlog.Errorf("clock: runClockCheck: Error while fetching clock data: %v", err)
		return err
	}
	if checkSystemRebooted(tx, c, clockData) {
		// System was rebooted. checkSystemRebooted() has already written new
		// data for vclock.
		return nil
	}

	systemTime := c.SysClock.Now()
	elapsedTime, err := c.SysClock.ElapsedTime()
	if err != nil {
		vlog.Errorf("clock: runClockCheck: Error while fetching elapsed time: %v", err)
		return err
	}

	newClockData := &ClockData{
		SystemTimeAtBoot:     clockData.SystemTimeAtBoot,
		Skew:                 clockData.Skew,
		ElapsedTimeSinceBoot: elapsedTime.Nanoseconds(),
		LastNtpTs:            clockData.LastNtpTs,
		NumReboots:           clockData.NumReboots,
		NumHops:              clockData.NumHops,
	}

	estimatedClockTime := clockData.SystemBootTime().Add(elapsedTime)
	diff := estimatedClockTime.Sub(systemTime)
	if math.Abs(float64(diff.Nanoseconds())) > util.LocalClockDriftThreshold {
		vlog.VI(2).Infof("clock: runClockCheck: clock drift of %v discovered.", diff)
		newClockData.Skew = newClockData.Skew + diff.Nanoseconds()
		newSystemTimeAtBoot := systemTime.Add(-elapsedTime)
		newClockData.SystemTimeAtBoot = newSystemTimeAtBoot.UnixNano()
	}

	if err := c.SetClockData(tx, newClockData); err != nil {
		vlog.Errorf("clock: runClockCheck: Error while setting clock data: %v", err)
		return err
	}
	return nil
}

// writeNewClockData overwrites existing clock data with the given params.
// It recreates system boot time based on current system clock and elapsed time.
// This method takes StoreWriter instead of Transaction to allow tests to pass
// a store.Store object instead for simplicity.
func writeNewClockData(tx store.Transaction, c *VClock, skew time.Duration, ntpTs *time.Time) {
	systemTime := c.SysClock.Now()
	elapsedTime, err := c.SysClock.ElapsedTime()
	if err != nil {
		vlog.Errorf("clock: Error while fetching elapsed time: %v", err)
		return
	}
	systemTimeAtBoot := systemTime.Add(-elapsedTime)
	clockData := &ClockData{
		SystemTimeAtBoot:     systemTimeAtBoot.UnixNano(),
		Skew:                 skew.Nanoseconds(),
		ElapsedTimeSinceBoot: elapsedTime.Nanoseconds(),
		LastNtpTs:            ntpTs,
		NumReboots:           0,
		NumHops:              0,
	}
	if err := c.SetClockData(tx, clockData); err != nil {
		vlog.Errorf("clock: Error while setting clock data: %v", err)
	}
}

// checkSystemRebooted compares the elapsed time stored during the last
// run of runClockCheck() to the current elapsed time since boot provided
// by system clock. Since elapsed time is monotonically increasing and cannot
// be changed unless a reboot happens, if the current value is lower than the
// previous value then a reboot has happened since last run. If so, update
// the boot time and elapsed time since boot appropriately.
func checkSystemRebooted(tx store.Transaction, c *VClock, clockData *ClockData) bool {
	currentSysTime := c.SysClock.Now()
	elapsedTime, err := c.SysClock.ElapsedTime()
	if err != nil {
		vlog.Errorf("clock: runClockCheck: Error while fetching elapsed time: %v", err)
		return false
	}

	if elapsedTime.Nanoseconds() < clockData.ElapsedTimeSinceBoot {
		// Since the elapsed time since last boot provided by the system is
		// less than the elapsed time since boot seen the last time clockservice
		// ran, the system must have rebooted in between.
		vlog.VI(2).Info("clock: runClockCheck: system reboot discovered")
		clockData.SystemTimeAtBoot = currentSysTime.Add(-elapsedTime).UnixNano()
		clockData.ElapsedTimeSinceBoot = elapsedTime.Nanoseconds()
		clockData.NumReboots += 1
		if err := c.SetClockData(tx, clockData); err != nil {
			vlog.Errorf("clock: runClockCheck: Error while setting clock data: %v", err)
		}
		return true
	}
	return false
}
