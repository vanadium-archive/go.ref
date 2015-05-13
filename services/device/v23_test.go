// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package device_test

import "testing"
import "os"

import "v.io/x/ref/test"
import "v.io/x/ref/test/v23tests"

func TestMain(m *testing.M) {
	test.Init()
	cleanup := v23tests.UseSharedBinDir()
	r := m.Run()
	cleanup()
	os.Exit(r)
}

func TestV23DeviceManager(t *testing.T) {
	v23tests.RunTest(t, V23TestDeviceManager)
}

func TestV23DeviceManagerMultiUser(t *testing.T) {
	v23tests.RunTest(t, V23TestDeviceManagerMultiUser)
}
