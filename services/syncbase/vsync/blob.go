// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"io"

	wire "v.io/syncbase/v23/services/syncbase/nosql"
	blob "v.io/syncbase/x/ref/services/syncbase/localblobstore"
	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

const (
	chunkSize = 8 * 1024
)

// blobLocInfo contains the location information about a BlobRef. This location
// information is merely a hint used to search for the blob.
type blobLocInfo struct {
	peer   string                          // Syncbase from which the presence of this BlobRef was first learned.
	source string                          // Syncbase that originated this blob.
	sgIds  map[interfaces.GroupId]struct{} // SyncGroups through which the BlobRef was learned.
}

////////////////////////////////////////////////////////////
// RPCs for managing blobs between Syncbase and its clients.

func (sd *syncDatabase) CreateBlob(ctx *context.T, call rpc.ServerCall) (wire.BlobRef, error) {
	vlog.VI(2).Infof("sync: CreateBlob: begin")
	defer vlog.VI(2).Infof("sync: CreateBlob: end")

	// Get this Syncbase's blob store handle.
	ss := sd.sync.(*syncService)
	bst := ss.bst

	writer, err := bst.NewBlobWriter(ctx, "")
	if err != nil {
		return wire.NullBlobRef, err
	}
	defer writer.CloseWithoutFinalize()

	name := writer.Name()
	vlog.VI(4).Infof("sync: CreateBlob: blob ref %s", name)
	return wire.BlobRef(name), nil
}

func (sd *syncDatabase) PutBlob(ctx *context.T, call wire.BlobManagerPutBlobServerCall, br wire.BlobRef) error {
	vlog.VI(2).Infof("sync: PutBlob: begin br %v", br)
	defer vlog.VI(2).Infof("sync: PutBlob: end br %v", br)

	// Get this Syncbase's blob store handle.
	ss := sd.sync.(*syncService)
	bst := ss.bst

	writer, err := bst.ResumeBlobWriter(ctx, string(br))
	if err != nil {
		return err
	}
	defer writer.CloseWithoutFinalize()

	stream := call.RecvStream()
	for stream.Advance() {
		item := blob.BlockOrFile{Block: stream.Value()}
		if err = writer.AppendFragment(item); err != nil {
			return err
		}
	}
	return stream.Err()
}

func (sd *syncDatabase) CommitBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) error {
	vlog.VI(2).Infof("sync: CommitBlob: begin br %v", br)
	defer vlog.VI(2).Infof("sync: CommitBlob: end br %v", br)

	// Get this Syncbase's blob store handle.
	ss := sd.sync.(*syncService)
	bst := ss.bst

	writer, err := bst.ResumeBlobWriter(ctx, string(br))
	if err != nil {
		return err
	}
	return writer.Close()
}

func (sd *syncDatabase) GetBlobSize(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) (int64, error) {
	vlog.VI(2).Infof("sync: GetBlobSize: begin br %v", br)
	defer vlog.VI(2).Infof("sync: GetBlobSize: end br %v", br)

	// Get this Syncbase's blob store handle.
	ss := sd.sync.(*syncService)
	bst := ss.bst

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
	vlog.VI(2).Infof("sync: GetBlob: begin br %v", br)
	defer vlog.VI(2).Infof("sync: GetBlob: end br %v", br)

	// First get the blob locally if available.
	ss := sd.sync.(*syncService)
	err := getLocalBlob(ctx, call.SendStream(), ss.bst, br, offset)
	if err == nil || verror.ErrorID(err) == wire.ErrBlobNotCommitted.ID {
		return err
	}

	return sd.fetchBlobRemote(ctx, br, nil, call, offset)
}

func (sd *syncDatabase) FetchBlob(ctx *context.T, call wire.BlobManagerFetchBlobServerCall, br wire.BlobRef, priority uint64) error {
	vlog.VI(2).Infof("sync: FetchBlob: begin br %v", br)
	defer vlog.VI(2).Infof("sync: FetchBlob: end br %v", br)

	clientStream := call.SendStream()

	// Check if BlobRef already exists locally.
	ss := sd.sync.(*syncService)
	bst := ss.bst

	bReader, err := bst.NewBlobReader(ctx, string(br))
	if err == nil {
		finalized := bReader.IsFinalized()
		bReader.Close()

		if !finalized {
			return wire.NewErrBlobNotCommitted(ctx)
		}
		clientStream.Send(wire.BlobFetchStatus{State: wire.BlobFetchStateDone})
		return nil
	}

	// Wait for this blob's turn.
	// TODO(hpucha): Implement a blob queue.
	clientStream.Send(wire.BlobFetchStatus{State: wire.BlobFetchStatePending})

	return sd.fetchBlobRemote(ctx, br, call, nil, 0)
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

func (s *syncService) FetchBlob(ctx *context.T, call interfaces.SyncFetchBlobServerCall, br wire.BlobRef) error {
	vlog.VI(2).Infof("sync: FetchBlob: sb-sb begin br %v", br)
	defer vlog.VI(2).Infof("sync: FetchBlob: sb-sb end br %v", br)
	return getLocalBlob(ctx, call.SendStream(), s.bst, br, 0)
}

func (s *syncService) HaveBlob(ctx *context.T, call rpc.ServerCall, br wire.BlobRef) (int64, error) {
	vlog.VI(2).Infof("sync: HaveBlob: begin br %v", br)
	defer vlog.VI(2).Infof("sync: HaveBlob: end br %v", br)

	bReader, err := s.bst.NewBlobReader(ctx, string(br))
	if err != nil {
		return 0, err
	}
	defer bReader.Close()
	if !bReader.IsFinalized() {
		return 0, wire.NewErrBlobNotCommitted(ctx)
	}
	return bReader.Size(), nil
}

func (s *syncService) FetchBlobRecipe(ctx *context.T, call interfaces.SyncFetchBlobRecipeServerCall, br wire.BlobRef) error {
	return verror.NewErrNotImplemented(ctx)
}

func (s *syncService) FetchChunks(ctx *context.T, call interfaces.SyncFetchChunksServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}

////////////////////////////////////////////////////////////
// Helpers.

type byteStream interface {
	Send(item []byte) error
}

// getLocalBlob looks for a blob in the local store and, if found, reads the
// blob and sends it to the client.  If the blob is found, it starts reading it
// from the given offset and sends its bytes into the client stream.
func getLocalBlob(ctx *context.T, stream byteStream, bst blob.BlobStore, br wire.BlobRef, offset int64) error {
	vlog.VI(4).Infof("sync: getLocalBlob: begin br %v, offset %v", br, offset)
	defer vlog.VI(4).Infof("sync: getLocalBlob: end br %v, offset %v", br, offset)

	reader, err := bst.NewBlobReader(ctx, string(br))
	if err != nil {
		return err
	}
	defer reader.Close()

	if !reader.IsFinalized() {
		return wire.NewErrBlobNotCommitted(ctx)
	}

	buf := make([]byte, chunkSize)
	for {
		nbytes, err := reader.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return err
		}
		if nbytes <= 0 {
			break
		}
		offset += int64(nbytes)
		stream.Send(buf[:nbytes])
		if err == io.EOF {
			break
		}
	}

	return nil
}

func (sd *syncDatabase) fetchBlobRemote(ctx *context.T, br wire.BlobRef, statusCall wire.BlobManagerFetchBlobServerCall, dataCall wire.BlobManagerGetBlobServerCall, offset int64) error {
	vlog.VI(4).Infof("sync: fetchBlobRemote: begin br %v, offset %v", br, offset)
	defer vlog.VI(4).Infof("sync: fetchBlobRemote: end br %v, offset %v", br, offset)

	var sendStatus, sendData bool
	var statusStream interface {
		Send(item wire.BlobFetchStatus) error
	}
	var dataStream interface {
		Send(item []byte) error
	}

	if statusCall != nil {
		sendStatus = true
		statusStream = statusCall.SendStream()
	}
	if dataCall != nil {
		sendData = true
		dataStream = dataCall.SendStream()
	}

	if sendStatus {
		// Start blob source discovery.
		statusStream.Send(wire.BlobFetchStatus{State: wire.BlobFetchStateLocating})
	}

	// Locate blob.
	peer, size, err := sd.locateBlob(ctx, br)
	if err != nil {
		return err
	}

	// Start blob fetching.
	status := wire.BlobFetchStatus{State: wire.BlobFetchStateFetching, Total: size}
	if sendStatus {
		statusStream.Send(status)
	}

	ss := sd.sync.(*syncService)
	bst := ss.bst

	bWriter, err := bst.NewBlobWriter(ctx, string(br))
	if err != nil {
		return err
	}

	c := interfaces.SyncClient(peer)
	ctxPeer, cancel := context.WithRootCancel(ctx)
	stream, err := c.FetchBlob(ctxPeer, br)
	if err == nil {
		peerStream := stream.RecvStream()
		for peerStream.Advance() {
			item := blob.BlockOrFile{Block: peerStream.Value()}
			if err = bWriter.AppendFragment(item); err != nil {
				break
			}
			curSize := int64(len(item.Block))
			status.Received += curSize
			if sendStatus {
				statusStream.Send(status)
			}
			if sendData {
				if curSize <= offset {
					offset -= curSize
				} else if offset != 0 {
					dataStream.Send(item.Block[offset:])
					offset = 0
				} else {
					dataStream.Send(item.Block)
				}
			}
		}

		if err != nil {
			cancel()
			stream.Finish()
		} else {
			err = peerStream.Err()
			if terr := stream.Finish(); err == nil {
				err = terr
			}
			cancel()
		}
	}

	bWriter.Close()
	if err != nil {
		// Clean up the blob with failed download, so that it can be
		// downloaded again. Ignore any error from deletion.
		bst.DeleteBlob(ctx, string(br))
	} else {
		status := wire.BlobFetchStatus{State: wire.BlobFetchStateDone}
		if sendStatus {
			statusStream.Send(status)
		}
	}
	return err
}

// TODO(hpucha): Add syncgroup driven blob discovery.
func (sd *syncDatabase) locateBlob(ctx *context.T, br wire.BlobRef) (string, int64, error) {
	vlog.VI(4).Infof("sync: locateBlob: begin br %v", br)
	defer vlog.VI(4).Infof("sync: locateBlob: end br %v", br)

	ss := sd.sync.(*syncService)
	loc, err := ss.getBlobLocInfo(ctx, br)
	if err != nil {
		return "", 0, err
	}

	// Search for blob amongst the source peer and peer learned from.
	var peers = []string{loc.source, loc.peer}
	for _, p := range peers {
		vlog.VI(4).Infof("sync: locateBlob: attempting %s", p)
		// Get the mounttables for this peer.
		mtTables, err := sd.getMountTables(ctx, p)
		if err != nil {
			continue
		}

		for mt := range mtTables {
			absName := naming.Join(mt, p, util.SyncbaseSuffix)
			c := interfaces.SyncClient(absName)
			size, err := c.HaveBlob(ctx, br)
			if err == nil {
				vlog.VI(4).Infof("sync: locateBlob: found blob on %s", absName)
				return absName, size, nil
			}
		}
	}

	return "", 0, verror.New(verror.ErrInternal, ctx, "blob not found")

}

func (sd *syncDatabase) getMountTables(ctx *context.T, peer string) (map[string]struct{}, error) {
	ss := sd.sync.(*syncService)
	mInfo := ss.copyMemberInfo(ctx, peer)

	mtTables := make(map[string]struct{})
	for gdbName, sgInfo := range mInfo.db2sg {
		appName, dbName, err := splitAppDbName(ctx, gdbName)
		if err != nil {
			return nil, err
		}
		st, err := ss.getDbStore(ctx, nil, appName, dbName)
		if err != nil {
			return nil, err
		}

		for id := range sgInfo {
			sg, err := getSyncGroupById(ctx, st, id)
			if err != nil {
				continue
			}
			if _, ok := sg.Joiners[peer]; !ok {
				// Peer is no longer part of the SyncGroup.
				continue
			}
			for _, mt := range sg.Spec.MountTables {
				mtTables[mt] = struct{}{}
			}
		}
	}
	return mtTables, nil
}

// TODO(hpucha): Persist the blob directory periodically.
func (s *syncService) addBlobLocInfo(ctx *context.T, br wire.BlobRef, info *blobLocInfo) error {
	s.blobDirLock.Lock()
	defer s.blobDirLock.Unlock()

	s.blobDirectory[br] = info
	return nil
}

func (s *syncService) getBlobLocInfo(ctx *context.T, br wire.BlobRef) (*blobLocInfo, error) {
	s.blobDirLock.Lock()
	defer s.blobDirLock.Unlock()

	if info, ok := s.blobDirectory[br]; ok {
		return info, nil
	}
	return nil, verror.New(verror.ErrInternal, ctx, "blob state not found", br)
}
