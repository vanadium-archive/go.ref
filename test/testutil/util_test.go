// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"regexp"
	"testing"
)

func TestFormatLogline(t *testing.T) {
	line, want := FormatLogLine(2, "test"), "testing.go:.*"
	if ok, err := regexp.MatchString(want, line); !ok || err != nil {
		t.Errorf("got %v, want %v", line, want)
	}
}
