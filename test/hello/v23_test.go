// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package hello_test

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

func TestV23HelloDirect(t *testing.T) {
	v23tests.RunTest(t, V23TestHelloDirect)
}

func TestV23HelloAgentd(t *testing.T) {
	v23tests.RunTest(t, V23TestHelloAgentd)
}

func TestV23HelloMounttabled(t *testing.T) {
	v23tests.RunTest(t, V23TestHelloMounttabled)
}

func TestV23HelloProxy(t *testing.T) {
	v23tests.RunTest(t, V23TestHelloProxy)
}
