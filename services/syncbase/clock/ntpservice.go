// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"math"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
)

// runNtpCheck talks to an NTP server, fetches the current UTC time from it
// and corrects VClock time.
func (c *VClock) runNtpCheck(ctx *context.T) error {
	ntpData, err := c.ntpSource.NtpSync(util.NtpSampleCount)
	if err != nil {
		vlog.Errorf("Error while fetching ntp time: %v", err)
		return err
	}
	offset := ntpData.offset

	data := &ClockData{}
	if err := c.sa.GetClockData(ctx, data); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			// No ClockData found, write a new one.
			writeNewClockData(ctx, c, offset)
			return nil
		}
		vlog.Info("Error while fetching clock data: %v", err)
		vlog.Info("Overwriting clock data with NTP")
		writeNewClockData(ctx, c, offset)
		return nil
	}

	// Update clock skew if the difference between offset and skew is larger
	// than NtpDiffThreshold. NtpDiffThreshold helps avoid constant tweaking of
	// the syncbase clock.
	if math.Abs(float64(offset.Nanoseconds()-data.Skew)) > util.NtpDiffThreshold {
		writeNewClockData(ctx, c, offset)
	}
	return nil
}
