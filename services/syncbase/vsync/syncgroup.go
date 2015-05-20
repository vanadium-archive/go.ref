// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

////////////////////////////////////////////////////////////
// SyncGroup methods between Client and Syncbase.

////////////////////////////////////////////////////////////
// Methods for SyncGroup create/join between Syncbases.

func (s *syncService) CreateSyncGroup(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}

func (s *syncService) JoinSyncGroup(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}
