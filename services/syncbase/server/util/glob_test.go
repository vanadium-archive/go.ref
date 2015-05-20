// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Note, we use package util rather than util_test so that we can access the
// unexported name util.globPatternToPrefix.

package util

import (
	"testing"

	"v.io/v23/verror"
)

var (
	errBadArg = string(verror.ErrBadArg.ID)
)

func TestGlobPatternToPrefix(t *testing.T) {
	tests := []struct {
		pattern, prefix string
	}{
		{"", errBadArg},
		{"*", ""},
		{"foo", errBadArg},
		{"foo*", "foo"},
		{"foo/", errBadArg},
		{"foo/*", errBadArg},
		{"foo/bar", errBadArg},
		{"foo/bar*", errBadArg},
		{".*", "."},
		{"..*", ".."},
		{"...*", errBadArg},
		{"...foo*", errBadArg},
		{"foo...foo*", errBadArg},
		{"..foo*", "..foo"},
		{"foo..foo*", "foo..foo"},
		{"...", errBadArg},
		{"*/...", errBadArg},
		{"foo/...", errBadArg},
		{"/", errBadArg},
		{"/*", errBadArg},
	}
	for _, test := range tests {
		prefix, err := globPatternToPrefix(test.pattern)
		if err != nil {
			prefix = string(verror.ErrorID(err))
		}
		if prefix != test.prefix {
			t.Errorf("%q: got %q, want %q", test.pattern, prefix, test.prefix)
		}
	}
}
