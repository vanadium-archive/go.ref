// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package common

import (
	"time"
)

// Clock is an interface to a generic clock.
type Clock interface {
	// Now returns the current time.
	Now() (time.Time, error)
}
