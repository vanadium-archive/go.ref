// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"os"
	"testing"

	"v.io/x/ref/lib/v23test"
	"v.io/x/ref/test/modules"
)

func TestMain(m *testing.M) {
	modules.DispatchAndExitIfChild()
	os.Exit(v23test.Run(m.Run))
}
