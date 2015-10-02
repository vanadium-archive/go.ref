// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

import "testing"

func TestPrefixGenVectorCompare(t *testing.T) {
	tests := []struct {
		a, b  PrefixGenVector
		resAB int
		resBA int
	}{
		{ // a = b.
			a:     PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			b:     PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			resAB: 0,
			resBA: 0,
		},
		{ // a = b.
			a:     PrefixGenVector{},
			b:     PrefixGenVector{},
			resAB: 0,
			resBA: 0,
		},
		{ // a > b.
			a:     PrefixGenVector{10: 10, 11: 11, 12: 12, 13: 13},
			b:     PrefixGenVector{10: 1, 11: 2, 12: 3, 13: 4},
			resAB: 1,
			resBA: -1,
		},
		{ // a > b.
			a:     PrefixGenVector{10: 38, 11: 5, 12: 56, 13: 13},
			b:     PrefixGenVector{},
			resAB: 1,
			resBA: -1,
		},
		{ // a > b.
			a:     PrefixGenVector{10: 10, 11: 11, 12: 12, 13: 13},
			b:     PrefixGenVector{11: 2, 12: 3, 13: 4},
			resAB: 1,
			resBA: -1,
		},
		{ // a > b.
			a:     PrefixGenVector{10: 10, 11: 11, 12: 12, 13: 13},
			b:     PrefixGenVector{11: 11, 12: 2, 13: 4},
			resAB: 1,
			resBA: -1,
		},
		{ // a > b.
			a:     PrefixGenVector{10: 10, 11: 11, 12: 12, 13: 13},
			b:     PrefixGenVector{11: 11, 12: 12, 13: 13},
			resAB: 1,
			resBA: -1,
		},
		{ // a > b.
			a:     PrefixGenVector{10: 1, 11: 11, 12: 12, 13: 13},
			b:     PrefixGenVector{10: 1, 11: 2, 12: 3, 13: 4},
			resAB: 1,
			resBA: -1,
		},
		{ // a > b.
			a:     PrefixGenVector{10: 38, 11: 5, 12: 56, 13: 13},
			b:     PrefixGenVector{10: 1, 11: 5, 12: 23, 13: 4},
			resAB: 1,
			resBA: -1,
		},
		{ // a > b.
			a:     PrefixGenVector{10: 0, 11: 5, 12: 56, 13: 13},
			b:     PrefixGenVector{11: 5, 12: 23, 13: 4},
			resAB: 1,
			resBA: -1,
		},
		{ // a != b.
			a:     PrefixGenVector{10: 38, 11: 5, 12: 56, 13: 13},
			b:     PrefixGenVector{10: 56, 11: 5, 12: 23, 13: 4},
			resAB: 2,
			resBA: 2,
		},
		{ // a != b.
			a:     PrefixGenVector{10: 38, 11: 5, 12: 56, 13: 13},
			b:     PrefixGenVector{10: 1, 11: 50, 12: 23, 13: 4},
			resAB: 2,
			resBA: 2,
		},
		{ // a != b.
			a:     PrefixGenVector{10: 10, 11: 11, 12: 12, 13: 13},
			b:     PrefixGenVector{11: 11, 12: 2, 13: 4, 15: 40},
			resAB: 2,
			resBA: 2,
		},
	}

	for pos, test := range tests {
		got, want := test.a.Compare(test.b), test.resAB
		if got != want {
			t.Fatalf("Comparison failed for pos %d (a=%v, b=%v), got %v, want %v", pos, test.a, test.b, got, want)
		}
		got, want = test.b.Compare(test.a), test.resBA
		if got != want {
			t.Fatalf("Comparison failed for pos %d (a=%v, b=%v), got %v, want %v", pos, test.a, test.b, got, want)
		}
	}
}
