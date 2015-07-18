// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"io"

	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/localblobstore"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

const (
	chunkSize = 8 * 1024
)

////////////////////////////////////////////////////////////
// RPCs for managing blobs between Syncbase and its clients.

func (sd *syncDatabase) CreateBlob(ctx *context.T, call rpc.ServerCall) (wire.BlobRef, error) {
	bst := sd.db.BlobSt()
	writer, err := bst.NewBlobWriter(ctx, "")
	if err != nil {
		return wire.NullBlobRef, err
	}
	defer writer.CloseWithoutFinalize()

	name := writer.Name()
	vlog.VI(2).Infof("sync: CreateBlob: blob ref %s", name)
	return wire.BlobRef(name), nil
}

func (sd *syncDatabase) PutBlob(ctx *context.T, call wire.BlobManagerPutBlobServerCall, br wire.BlobRef) error {
	bst := sd.db.BlobSt()
	writer, err := bst.NewBlobWriter(ctx, string(br))
	if err != nil {
		return err
	}
	defer writer.CloseWithoutFinalize()

	stream := call.RecvStream()
	for stream.Advance() {
		item := localblobstore.BlockOrFile{Block: stream.Value()}
		if err = writer.AppendFragment(item); err != nil {
			return err
		}
	}
	return stream.Err()
}

func (sd *syncDatabase) CommitBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) error {
	bst := sd.db.BlobSt()
	writer, err := bst.NewBlobWriter(ctx, string(br))
	if err != nil {
		return err
	}
	return writer.Close()
}

func (sd *syncDatabase) GetBlobSize(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) (int64, error) {
	bst := sd.db.BlobSt()
	reader, err := bst.NewBlobReader(ctx, string(br))
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	return reader.Size(), nil
}

func (sd *syncDatabase) DeleteBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) error {
	return verror.NewErrNotImplemented(ctx)
}

func (sd *syncDatabase) GetBlob(ctx *context.T, call wire.BlobManagerGetBlobServerCall, br wire.BlobRef, offset int64) error {
	// First get the blob locally if available.
	err := sd.getLocalBlob(ctx, call, br, offset)
	if err == nil {
		return nil
	}

	// Locate the blob on a remote blob store and fetch it.
	return verror.NewErrNotImplemented(ctx)
}

// getLocalBlob looks for a blob in the local store and, if found, reads the
// blob and sends it to the client.  If the blob is found, it starts reading it
// from the given offset and sends its bytes into the client stream.
func (sd *syncDatabase) getLocalBlob(ctx *context.T, call wire.BlobManagerGetBlobServerCall, br wire.BlobRef, offset int64) error {
	bst := sd.db.BlobSt()
	reader, err := bst.NewBlobReader(ctx, string(br))
	if err != nil {
		return err
	}
	defer reader.Close()

	buf := make([]byte, chunkSize)
	stream := call.SendStream()

	for {
		nbytes, err := reader.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return err
		}
		if err == io.EOF || nbytes <= 0 {
			break
		}
		offset += int64(nbytes)
		stream.Send(buf[:nbytes])
	}

	return nil
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
