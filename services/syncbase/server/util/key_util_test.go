// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util_test

import (
	"reflect"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
)

type kpt struct {
	parts []string
	key   string
}

var keyPartTests []kpt = []kpt{
	{[]string{"a", "b"}, "a:b"},
	{[]string{"aa", "bb"}, "aa:bb"},
	{[]string{"a", "b", "c"}, "a:b:c"},
}

func TestJoinKeyParts(t *testing.T) {
	for _, test := range keyPartTests {
		got, want := util.JoinKeyParts(test.parts...), test.key
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%v: got %q, want %q", test.parts, got, want)
		}
	}
}

func TestSplitKeyParts(t *testing.T) {
	for _, test := range keyPartTests {
		got, want := util.SplitKeyParts(test.key), test.parts
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v", test.key, got, want)
		}
	}
}

func TestScanPrefixArgs(t *testing.T) {
	tests := []struct {
		stKeyPrefix, prefix, wantStart, wantLimit string
	}{
		{"x", "", "x:", "x;"},
		{"x", "a", "x:a", "x:b"},
		{"x", "a\xff", "x:a\xff", "x:b"},
	}
	for _, test := range tests {
		start, limit := util.ScanPrefixArgs(test.stKeyPrefix, test.prefix)
		gotStart, gotLimit := string(start), string(limit)
		if gotStart != test.wantStart {
			t.Errorf("{%q, %q} start: got %q, want %q", test.stKeyPrefix, test.prefix, gotStart, test.wantStart)
		}
		if gotLimit != test.wantLimit {
			t.Errorf("{%q, %q} limit: got %q, want %q", test.stKeyPrefix, test.prefix, gotLimit, test.wantLimit)
		}
	}
}

func TestScanRangeArgs(t *testing.T) {
	tests := []struct {
		stKeyPrefix, start, limit, wantStart, wantLimit string
	}{
		{"x", "", "", "x:", "x;"},   // limit "" means "no limit"
		{"x", "a", "", "x:a", "x;"}, // limit "" means "no limit"
		{"x", "a", "b", "x:a", "x:b"},
		{"x", "a", "a", "x:a", "x:a"}, // empty range
		{"x", "b", "a", "x:b", "x:a"}, // empty range
	}
	for _, test := range tests {
		start, limit := util.ScanRangeArgs(test.stKeyPrefix, test.start, test.limit)
		gotStart, gotLimit := string(start), string(limit)
		if gotStart != test.wantStart {
			t.Errorf("{%q, %q, %q} start: got %q, want %q", test.stKeyPrefix, test.start, test.limit, gotStart, test.wantStart)
		}
		if gotLimit != test.wantLimit {
			t.Errorf("{%q, %q, %q} limit: got %q, want %q", test.stKeyPrefix, test.start, test.limit, gotLimit, test.wantLimit)
		}
	}
}
