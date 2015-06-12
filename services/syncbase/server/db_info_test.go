// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"testing"
)

type stk struct {
	appName string
	dbName  string
	stkey   string
}

var stTestKey stk = stk{"app1", "db1", "$dbInfo:app1:db1"}

var dbinfo *dbInfoLayer = &dbInfoLayer{
	name: stTestKey.dbName,
	a: &app{
		name: stTestKey.appName,
	},
}

func TestStKey(t *testing.T) {
	if stTestKey.stkey != dbinfo.StKey() {
		t.Errorf("dbInfoLayer stkey expected to be %q but found to be %q", stTestKey.stkey, dbinfo.StKey())
	}
}
