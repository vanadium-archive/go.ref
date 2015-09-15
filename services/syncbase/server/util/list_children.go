// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/ref/services/syncbase/store"
)

// ListChildren returns the names of all apps, databases, or tables with the
// given key prefix. Designed for use by Service.ListApps, App.ListDatabases,
// and Database.ListTables.
func ListChildren(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction, stKeyPrefix string) ([]string, error) {
	it := sntx.Scan(ScanPrefixArgs(stKeyPrefix, ""))
	key := []byte{}
	res := []string{}
	for it.Advance() {
		key = it.Key(key)
		parts := SplitKeyParts(string(key))
		res = append(res, parts[len(parts)-1])
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return res, nil
}
