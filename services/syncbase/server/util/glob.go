// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"
)

// NOTE(nlacasse): Syncbase handles Glob requests by implementing
// GlobChildren__ at each level (service, app, database, table).

// Glob performs a glob. It calls closeSntx to close sntx.
func Glob(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element, sntx store.SnapshotOrTransaction, closeSntx func() error, stKeyPrefix string) error {
	prefix, _ := matcher.FixedPrefix()
	it := sntx.Scan(ScanPrefixArgs(stKeyPrefix, prefix))
	defer closeSntx()
	key := []byte{}
	for it.Advance() {
		key = it.Key(key)
		parts := SplitKeyParts(string(key))
		name := parts[len(parts)-1]
		if matcher.Match(name) {
			if err := call.SendStream().Send(naming.GlobChildrenReplyName{name}); err != nil {
				return err
			}
		}
	}
	if err := it.Err(); err != nil {
		vlog.VI(1).Infof("Glob() failed: %v", err)
		call.SendStream().Send(naming.GlobChildrenReplyError{naming.GlobError{Error: err}})
	}
	return nil
}
