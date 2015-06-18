// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

// This interface provides a wrapper over system clock to allow easy testing
// of VClock and other code that uses timestamps. Tests can implement a mock
// SystemClock and set it on VClock using SetSystemClock() method.
type SystemClock interface {
	// Now returns the current UTC time in nanoseconds as known by the system.
	// This may not reflect the real UTC time if the system clock is out of
	// sync with UTC.
	Now() int64
}
