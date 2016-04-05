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
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/store"
)

// Note, Syncbase handles Glob requests by implementing GlobChildren__ at each
// level (service, app, database, collection).

// GlobChildren implements glob over the Syncbase namespace.
func GlobChildren(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element, sntx store.SnapshotOrTransaction, stKeyPrefix string) error {
	encPrefix, _ := matcher.FixedPrefix()
	prefix, err := pubutil.Decode(encPrefix)
	if err != nil {
		return verror.New(verror.ErrBadArg, ctx, err)
	}
	it := sntx.Scan(common.ScanPrefixArgs(stKeyPrefix, prefix))
	key := []byte{}
	for it.Advance() {
		key = it.Key(key)
		parts := common.SplitKeyParts(string(key))
		// For explanation of Encode(), see comment in server/dispatcher.go.
		name := pubutil.Encode(parts[len(parts)-1])
		if matcher.Match(name) {
			// TODO(rogulenko): Check for resolve access. (For collection glob, this
			// means checking prefix perms.)
			if err := call.SendStream().Send(naming.GlobChildrenReplyName{Value: name}); err != nil {
				return err
			}
		}
	}
	if err := it.Err(); err != nil {
		call.SendStream().Send(naming.GlobChildrenReplyError{Value: naming.GlobError{Error: err}})
	}
	return nil
}
