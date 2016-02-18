// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

import (
	"time"
)

// VClock is an internal interface to the vanadium clock.
type VClock interface {
	// Now returns our estimate of current UTC time based on SystemClock time and
	// our estimate of its skew relative to NTP time.
	Now() (time.Time, error)
}
