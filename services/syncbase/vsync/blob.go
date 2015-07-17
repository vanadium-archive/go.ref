// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

////////////////////////////////////////////////////////////
// RPCs for managing blobs between Syncbase and its clients.

func (sd *syncDatabase) CreateBlob(ctx *context.T, call rpc.ServerCall) (wire.BlobRef, error) {
	return wire.BlobRef(""), verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) PutBlob(ctx *context.T, call wire.BlobManagerPutBlobServerCall, br wire.BlobRef) error {
	return verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) CommitBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) error {
	return verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) GetBlobSize(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) (uint64, error) {
	return 0, verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) DeleteBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) error {
	return verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) GetBlob(ctx *context.T, call wire.BlobManagerGetBlobServerCall, br wire.BlobRef, offset uint64) error {
	return verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) FetchBlob(ctx *context.T, call wire.BlobManagerFetchBlobServerCall, br wire.BlobRef, priority uint64) error {
	return verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) PinBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) error {
	return verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) UnpinBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) error {
	return verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) KeepBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef, rank uint64) error {
	return verror.NewErrNotImplemented(ctx)
}

////////////////////////////////////////////////////////////
// RPC for blob fetch between Syncbases.

func (s *syncService) FetchBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) error {
	return verror.NewErrNotImplemented(ctx)
}
