// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lockfile

// StillRunning exports the internal function for the unittest.
func StillRunning(expected []byte) (error, bool) {
	return stillRunning(expected)
}
