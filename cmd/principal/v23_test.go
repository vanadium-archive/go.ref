// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package main_test

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

func TestV23BlessSelf(t *testing.T) {
	v23tests.RunTest(t, V23TestBlessSelf)
}

func TestV23Store(t *testing.T) {
	v23tests.RunTest(t, V23TestStore)
}

func TestV23Dump(t *testing.T) {
	v23tests.RunTest(t, V23TestDump)
}

func TestV23RecvBlessings(t *testing.T) {
	v23tests.RunTest(t, V23TestRecvBlessings)
}

func TestV23Fork(t *testing.T) {
	v23tests.RunTest(t, V23TestFork)
}

func TestV23Create(t *testing.T) {
	v23tests.RunTest(t, V23TestCreate)
}

func TestV23Caveats(t *testing.T) {
	v23tests.RunTest(t, V23TestCaveats)
}

func TestV23ForkWithoutVDLPATH(t *testing.T) {
	v23tests.RunTest(t, V23TestForkWithoutVDLPATH)
}

func TestV23ForkWithoutCaveats(t *testing.T) {
	v23tests.RunTest(t, V23TestForkWithoutCaveats)
}

func TestV23Bless(t *testing.T) {
	v23tests.RunTest(t, V23TestBless)
}

func TestV23AddToRoots(t *testing.T) {
	v23tests.RunTest(t, V23TestAddToRoots)
}
