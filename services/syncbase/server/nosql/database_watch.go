// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	nosqlwire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/services/watch"
	"v.io/v23/verror"
)

// WatchGlob implements the nosqlwire.DatabaseWatcher interface.
func (d *databaseReq) WatchGlob(ctx *context.T, call watch.GlobWatcherWatchGlobServerCall, req watch.GlobRequest) error {
	// TODO(rogulenko): Implement.
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId != nil {
		return nosqlwire.NewErrBoundToBatch(ctx)
	}
	return verror.NewErrNotImplemented(ctx)
}

// GetResumeMarker implements the nosqlwire.DatabaseWatcher interface.
func (d *databaseReq) GetResumeMarker(ctx *context.T, call rpc.ServerCall) (watch.ResumeMarker, error) {
	// TODO(rogulenko): Implement.
	if !d.exists {
		return nil, verror.New(verror.ErrNoExist, ctx, d.name)
	}
	return nil, verror.NewErrNotImplemented(ctx)
}
