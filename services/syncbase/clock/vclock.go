// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"time"
)

// VClock holds data required to provide an estimate of the UTC time at any
// given point. The fields contained in here are
// - systemTimeAtBoot : the time shown by the system clock at boot
// - utcTimeAtBoot    : the estimated UTC time when the system booted
// - skew             : the difference between the system clock and UTC time
// - clock            : Instance of clock.SystemClock interface providing access
//                      to the system time.
type VClock struct {
	systemTimeAtBoot time.Time
	utcTimeAtBoot    time.Time
	skew             time.Duration
	clock            SystemClock
}

func NewVClock() *VClock {
	return &VClock{
		clock: NewSystemClock(),
	}
}

// Now returns current UTC time based on the estimation of skew that
// the system clock has with respect to NTP.
func (c *VClock) Now() time.Time {
	// This method returns just the current system time for now.
	// TODO(jlodhia): implement estimation of UTC time.
	return c.clock.Now()
}

// This method allows tests to set a mock clock instance for testability.
func (c *VClock) SetSystemClock(sysClock SystemClock) {
	c.clock = sysClock
}

///////////////////////////////////////////////////
// Implementation for SystemClock

type systemClockImpl struct{}

func (sc *systemClockImpl) Now() time.Time {
	return time.Now().UTC()
}

var _ SystemClock = (*systemClockImpl)(nil)

func NewSystemClock() SystemClock {
	return &systemClockImpl{}
}
