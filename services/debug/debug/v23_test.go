// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

package main_test

import (
	"os"
	"testing"

	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

func TestMain(m *testing.M) {
	modules.DispatchAndExitIfChild()
	cleanup := v23tests.UseSharedBinDir()
	r := m.Run()
	cleanup()
	os.Exit(r)
}

func TestV23DebugGlob(t *testing.T) {
	v23tests.RunTest(t, V23TestDebugGlob)
}

func TestV23DebugGlobLogs(t *testing.T) {
	v23tests.RunTest(t, V23TestDebugGlobLogs)
}

func TestV23ReadHostname(t *testing.T) {
	v23tests.RunTest(t, V23TestReadHostname)
}

func TestV23LogSize(t *testing.T) {
	v23tests.RunTest(t, V23TestLogSize)
}

func TestV23StatsRead(t *testing.T) {
	v23tests.RunTest(t, V23TestStatsRead)
}

func TestV23StatsWatch(t *testing.T) {
	v23tests.RunTest(t, V23TestStatsWatch)
}

func TestV23VTrace(t *testing.T) {
	v23tests.RunTest(t, V23TestVTrace)
}

func TestV23Pprof(t *testing.T) {
	v23tests.RunTest(t, V23TestPprof)
}
