// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package device_test

import (
	"os"
	"os/user"

	"v.io/x/ref/test/v23tests"
)

const psFlags = "-ef"

func makeTestAccounts(i *v23tests.T) {
	userAddCmd := i.BinaryFromPath("/usr/bin/sudo")

	if _, err := user.Lookup("vana"); err != nil {
		userAddCmd.Start("/usr/sbin/adduser", "--no-create-home", "vana").WaitOrDie(os.Stdout, os.Stderr)
	}

	if _, err := user.Lookup("devicemanager"); err != nil {
		userAddCmd.Start("/usr/sbin/adduser", "--no-create-home", "devicemanager").Wait(os.Stdout, os.Stderr)
	}
}

const runTestOnThisPlatform = true
