// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

package main_test

import (
	"os"
	"testing"

	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

func TestMain(m *testing.M) {
	test.Init()
	modules.DispatchAndExitIfChild()
	cleanup := v23tests.UseSharedBinDir()
	r := m.Run()
	cleanup()
	os.Exit(r)
}

func TestV23GroupServerIntegration(t *testing.T) {
	v23tests.RunTest(t, V23TestGroupServerIntegration)
}

func TestV23GroupServerAuthorization(t *testing.T) {
	v23tests.RunTest(t, V23TestGroupServerAuthorization)
}