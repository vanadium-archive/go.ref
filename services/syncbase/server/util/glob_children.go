// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	pubutil "v.io/v23/syncbase/util"
	"v.io/x/ref/services/syncbase/store"
)

// Note, Syncbase handles Glob requests by implementing GlobChildren__ at each
// level (service, app, database, table).

// GlobChildren performs a glob. It calls closeSntx to close sntx.
func GlobChildren(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element, sntx store.SnapshotOrTransaction, closeSntx func() error, stKeyPrefix string) error {
	prefix, _ := matcher.FixedPrefix()
	it := sntx.Scan(ScanPrefixArgs(stKeyPrefix, prefix))
	defer closeSntx()
	key := []byte{}
	for it.Advance() {
		key = it.Key(key)
		parts := SplitKeyParts(string(key))
		name := parts[len(parts)-1]
		if matcher.Match(name) {
			// TODO(rogulenko): Check for resolve access. (For table glob, this means
			// checking prefix perms.)
			// For explanation of Escape(), see comment in server/nosql/dispatcher.go.
			if err := call.SendStream().Send(naming.GlobChildrenReplyName{Value: pubutil.Escape(name)}); err != nil {
				return err
			}
		}
	}
	if err := it.Err(); err != nil {
		call.SendStream().Send(naming.GlobChildrenReplyError{Value: naming.GlobError{Error: err}})
	}
	return nil
}
