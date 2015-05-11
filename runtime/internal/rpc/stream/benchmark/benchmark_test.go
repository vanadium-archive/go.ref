// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import (
	"os"
	"testing"

	"v.io/x/ref/test/benchmark"
)

// A single empty test to avoid:
// testing: warning: no tests to run
// from showing up when running benchmarks in this package via "go test"
func TestNoOp(t *testing.T) {}

func TestMain(m *testing.M) {
	os.Exit(benchmark.RunTestMain(m))
}
