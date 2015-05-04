// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server_test

// Note: Most of our unit tests are client-side and cover end-to-end behavior.
// Tests of the "server" package (and below) specifically target aspects of the
// implementation that are difficult to test from the client side.

import (
	"testing"

	tu "v.io/syncbase/v23/syncbase/testutil"
	_ "v.io/x/ref/profiles"
)

////////////////////////////////////////
// Test cases

// TODO(sadovsky): Write some tests.
func TestSomething(t *testing.T) {
	_, _, cleanup := tu.SetupOrDie(nil)
	defer cleanup()
}
