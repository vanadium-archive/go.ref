// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"math"
	"time"

	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// runNtpCheck talks to an NTP server, fetches the current UTC time from it
// and corrects VClock time.
func (c *VClock) runNtpCheck() {
	vlog.VI(2).Info("clock: runNtpCheck: starting ntp check")
	defer vlog.VI(2).Info("clock: runNtpCheck: ntp check finished")

	err := store.RunInTransaction(c.st, func(tx store.Transaction) error {
		return c.runNtpCheckInternal(tx)
	})
	if err != nil {
		vlog.Errorf("clock: runNtpCheck: ntp check run failed: %v", err)
	}
}

func (c *VClock) runNtpCheckInternal(tx store.Transaction) error {
	ntpData, err := c.ntpSource.NtpSync(util.NtpSampleCount)
	if err != nil {
		vlog.Errorf("clock: runNtpCheck: Error while fetching ntp time: %v", err)
		return err
	}
	offset := ntpData.offset
	vlog.VI(2).Infof("clock: runNtpCheck: offset is %v", offset)

	data := &ClockData{}
	if err := c.GetClockData(tx, data); err != nil {
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			vlog.Infof("clock: runNtpCheck: Error while fetching clock data: %v", err)
			vlog.Info("clock: runNtpCheck: Overwriting clock data with NTP")
		}
		// If no ClockData found or error while reading, write a new one.
		writeNewClockData(tx, c, offset, &ntpData.ntpTs)
		return nil
	}

	// Update clock skew if the difference between offset and skew is larger
	// than NtpDiffThreshold. NtpDiffThreshold helps avoid constant tweaking of
	// the syncbase clock.
	deviation := math.Abs(float64(offset.Nanoseconds() - data.Skew))
	if deviation > util.NtpDiffThreshold {
		vlog.VI(2).Infof("clock: runNtpCheck: clock found deviating from ntp by %v nsec. Updating clock.", deviation)
		writeNewClockData(tx, c, offset, &ntpData.ntpTs)
		return nil
	}

	// Update clock data with new LastNtpTs. We dont update the skew in db with
	// the latest offset because there will always be minor errors introduced
	// by asymmetrical network delays in measuring the offset leading to constant
	// change in clock skew.
	writeNewClockData(tx, c, time.Duration(data.Skew), &ntpData.ntpTs)
	return nil
}
