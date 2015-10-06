// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

// Clock utility functions.

import (
	"time"

	"v.io/x/lib/vlog"
)

// errorThreshold is kept at 2 seconds because there is an error margin of
// half a second while fetching elapsed time. This means that the combined
// error for fetching two elapsed timestamps can be up to 1 second.
var errorThreshold time.Duration = 2 * time.Second

// hasClockChanged checks if the system clock was changed between two
// timestamp samples taken from system clock.
// t1 is the first timestamp sampled from clock.
// e1 is the elapsed time since boot sampled before t1 was sampled.
// t2 is the second timestamp sampled from clock (t2 sampled after t1)
// e2 is the elapsed time since boot sampled after t2 was sampled.
// Note: e1 must be sampled before t1 and e2 must be sampled after t2.
func HasSysClockChanged(t1, t2 time.Time, e1, e2 time.Duration) bool {
	if t2.Before(t1) {
		vlog.VI(2).Infof("clock: hasSysClockChanged: t2 is before t1, returning true")
		return true
	}

	tsDiff := t2.Sub(t1)
	elapsedDiff := e2 - e1

	// Since elapsed time has an error margin of +/- half a second, elapsedDiff
	// can end up being smaller than tsDiff sometimes. Hence we take abs() of
	// the difference.
	return abs(elapsedDiff-tsDiff) > errorThreshold
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
