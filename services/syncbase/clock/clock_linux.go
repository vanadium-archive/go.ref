// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"syscall"
	"time"
)

// This file contains linux specific implementations of functions for clock
// package.

// Linux System stores this information in /proc/uptime as seconds
// since boot with a precision up to 2 decimal points.
// NOTE: Go system call returns elapsed time in seconds and removes the decimal
// points by rounding to the closest second. Be careful in using this value as
// it can introduce a compounding error.
func (sc *systemClockImpl) ElapsedTime() (time.Duration, error) {
	var sysInfo syscall.Sysinfo_t
	if err := syscall.Sysinfo(&sysInfo); err != nil {
		return 0, err
	}
	return time.Duration(sysInfo.Uptime) * time.Second, nil
}
