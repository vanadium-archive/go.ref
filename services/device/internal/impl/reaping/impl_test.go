// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reaping_test

import (
	"testing"

	"v.io/x/ref/services/device/internal/impl/utiltest"
)

func TestMain(m *testing.M) {
	utiltest.TestMainImpl(m)
}

// TestSuidHelper is testing boilerplate for suidhelper that does not
// create a runtime because the suidhelper is not a Vanadium application.
func TestSuidHelper(t *testing.T) {
	utiltest.TestSuidHelperImpl(t)
}
