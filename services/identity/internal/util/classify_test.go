// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"flag"
	"testing"
)

func TestEmailClassifier(t *testing.T) {
	fs := flag.NewFlagSet("TestEmailClassifier", flag.PanicOnError)
	var c EmailClassifier
	fs.Var(&c, "myflag", "my usage")
	if err := fs.Parse([]string{"--myflag", "foo.com=internal,bar.com=external"}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		in, out string
	}{
		{"batman@foo.com", "internal"},
		{"bugsbunny@foo.com.com", "users"},
		{"daffyduck@bar.com", "external"},
		{"joker@other.com", "users"},
	}
	for _, test := range tests {
		if got := c.Classify(test.in); got != test.out {
			t.Errorf("%q: Got %q, want %q", test.in, got, test.out)
		}
	}
}
