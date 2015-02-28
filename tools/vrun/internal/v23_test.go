// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package main_test

import "testing"
import "os"

import "v.io/x/ref/lib/testutil"
import "v.io/x/ref/lib/testutil/v23tests"

func TestMain(m *testing.M) {
	testutil.Init()
	cleanup := v23tests.UseSharedBinDir()
	r := m.Run()
	cleanup()
	os.Exit(r)
}

func TestV23Agentd(t *testing.T) {
	v23tests.RunTest(t, V23TestAgentd)
}
