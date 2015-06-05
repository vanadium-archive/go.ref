// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil_test

import (
	"regexp"
	"testing"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/testutil"
	"v.io/x/ref/test/v23tests"
)

func TestFormatLogline(t *testing.T) {
	line, want := testutil.FormatLogLine(2, "test"), "testing.go:.*"
	if ok, err := regexp.MatchString(want, line); !ok || err != nil {
		t.Errorf("got %v, want %v", line, want)
	}
}

//go:generate v23 test generate .

func V23TestRandSeed(i *v23tests.T) {
	v23bin := i.BinaryFromPath("v23")
	inv := v23bin.Start("go", "test", "./testdata")
	inv.ExpectRE("FAIL: TestRandSeed.*", 1)
	parts := inv.ExpectRE(`rand: (\d+)`, -1)
	if len(parts) != 1 || len(parts[0]) != 2 {
		i.Fatalf("failed to match regexp")
	}
	randInt := parts[0][1]
	parts = inv.ExpectRE(`Seeded pseudo-random number generator with (\d+)`, -1)
	if len(parts) != 1 || len(parts[0]) != 2 {
		i.Fatalf("failed to match regexp")
	}

	seed := parts[0][1]
	// Rerun the test, this time with the seed that we want to use.
	v23bin = v23bin.WithEnv("V23_RNG_SEED=" + seed)
	inv = v23bin.Start("go", "test", "./testdata")
	inv.ExpectRE("FAIL: TestRandSeed.*", 1)
	inv.ExpectRE("rand: "+randInt, 1)
	inv.ExpectRE("Seeded pseudo-random number generator with "+seed, -1)
}
