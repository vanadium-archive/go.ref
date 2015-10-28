// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"testing"
)

func TestStKey(t *testing.T) {
	tests := []struct {
		appName string
		dbName  string
		stKey   string
	}{
		{"app1", "db1", "i\xfeapp1\xfedb1"},
	}
	for _, test := range tests {
		got, want := dbInfoStKey(&app{name: test.appName}, test.dbName), test.stKey
		if got != want {
			t.Errorf("wrong stKey: got %q, want %q", got, want)
		}
	}
}
