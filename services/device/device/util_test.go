// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"v.io/v23"
	"v.io/v23/context"

	cmd_device "v.io/x/ref/services/device/device"
	"v.io/x/ref/test"
)

var gctx *context.T

func initTest() v23.Shutdown {
	var shutdown v23.Shutdown
	gctx, shutdown = test.InitForTest()
	cmd_device.SetGlobalContext(gctx)
	return func() {
		shutdown()
		cmd_device.SetGlobalContext(nil)
		gctx = nil
	}
}
