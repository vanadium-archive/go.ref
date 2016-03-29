// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"io"
	"strings"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/common"
	blob "v.io/x/ref/services/syncbase/localblobstore"
	"v.io/x/ref/services/syncbase/server/interfaces"
)

const (
	chunkSize = 8 * 1024
)

// blobLocInfo contains the location information about a BlobRef. This location
// information is merely a hint used to search for the blob.
type blobLocInfo struct {
	peer   string // Syncbase from which the presence of this BlobRef was first learned.
	source string // Syncbase that originated this blob.
	sgIds  sgSet  // Syncgroups through which the BlobRef was learned.
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
		if err = writer.AppendBytes(item); err != nil {
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
			if err = bWriter.AppendBytes(item); err != nil {
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
		// Get the mount tables for this peer.
		mtTables, err := sd.getMountTables(ctx, p)
		if err != nil {
			continue
		}

		for mt := range mtTables {
			absName := naming.Join(mt, p, common.SyncbaseSuffix)
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
	return mInfo.mtTables, nil
}

// TODO(hpucha): Persist the blob directory periodically.
func (s *syncService) addBlobLocInfo(ctx *context.T, br wire.BlobRef, info *blobLocInfo) error {
	s.blobDirLock.Lock()
	defer s.blobDirLock.Unlock()

	if curInfo := s.blobDirectory[br]; curInfo != nil {
		// Update the set of syncgroups but do not overwrite the peer
		// and source, the prior data is most likely higher quality.
		// For example, the same blob ref could appear in multiple
		// versions of an object learned from multiple peers, but the
		// first time it was injected into the object is more relevant.
		for id := range info.sgIds {
			curInfo.sgIds[id] = struct{}{}
		}
	} else {
		s.blobDirectory[br] = info
	}

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

// processBlobRefs decodes the VOM-encoded value in the buffer and extracts from
// it all blob refs.  For each of these blob refs, it updates the blob metadata
// to associate to it the sync peer, the source, and the matching syncgroups.
func (s *syncService) processBlobRefs(ctx *context.T, peer string, sgPrefixes map[string]sgSet, m *interfaces.LogRecMetadata, valbuf []byte) error {
	objid := m.ObjId
	srcPeer := syncbaseIdToName(m.Id)

	vlog.VI(4).Infof("sync: processBlobRefs: begin: objid %s, peer %s, src %s", objid, peer, srcPeer)
	defer vlog.VI(4).Infof("sync: processBlobRefs: end: objid %s, peer %s, src %s", objid, peer, srcPeer)

	if valbuf == nil {
		return nil
	}

	var val *vdl.Value
	if err := vom.Decode(valbuf, &val); err != nil {
		// If we cannot decode the value, ignore blob processing and
		// continue. This is fine since all stored values need not be
		// vom encoded.
		return nil
	}

	brs := extractBlobRefs(val)

	// The key (objid) starts with one of the store's reserved prefixes for
	// managed namespaces.  Remove that prefix to be able to compare it with
	// the syncgroup prefixes which are defined by the application.
	appKey := common.StripFirstKeyPartOrDie(objid)

	// Determine the set of syncgroups that cover this application key.
	sgIds := make(sgSet)
	for p, sgs := range sgPrefixes {
		if strings.HasPrefix(appKey, p) {
			for sg := range sgs {
				sgIds[sg] = struct{}{}
			}
		}
	}

	// Associate the blob metadata with each blob ref.  Create a separate
	// copy of the syncgroup set for each blob ref.
	for br := range brs {
		vlog.VI(4).Infof("sync: processBlobRefs: found blobref %v, sgs %v", br, sgIds)
		info := &blobLocInfo{peer: peer, source: srcPeer, sgIds: make(sgSet)}
		for gid := range sgIds {
			info.sgIds[gid] = struct{}{}
		}
		if err := s.addBlobLocInfo(ctx, br, info); err != nil {
			return err
		}
	}

	return nil
}

// extractBlobRefs traverses a VDL value and extracts blob refs from it.
func extractBlobRefs(val *vdl.Value) map[wire.BlobRef]struct{} {
	brs := make(map[wire.BlobRef]struct{})
	extractBlobRefsInternal(val, brs)
	return brs
}

// extractBlobRefsInternal traverses a VDL value recursively and extracts blob
// refs from it.  The blob refs are accumulated in the given map of blob refs.
// The function returns true if the data may contain blob refs, which means it
// either contains blob refs or contains dynamic data types (VDL union or any)
// which may in some instances contain blob refs.  Otherwise the function
// returns false to indicate that the data definitely cannot contain blob refs.
func extractBlobRefsInternal(val *vdl.Value, brs map[wire.BlobRef]struct{}) bool {
	mayContain := false

	if val != nil {
		switch val.Kind() {
		case vdl.String:
			// Could be a BlobRef.
			var br wire.BlobRef
			if val.Type() == vdl.TypeOf(br) {
				mayContain = true
				if b := wire.BlobRef(val.RawString()); b != wire.NullBlobRef {
					brs[b] = struct{}{}
				}
			}

		case vdl.Struct:
			for i := 0; i < val.Type().NumField(); i++ {
				if extractBlobRefsInternal(val.StructField(i), brs) {
					mayContain = true
				}
			}

		case vdl.Array, vdl.List:
			for i := 0; i < val.Len(); i++ {
				if extractBlobRefsInternal(val.Index(i), brs) {
					mayContain = true
				} else {
					// Look no further, no blob refs in the rest.
					break
				}
			}

		case vdl.Map:
			lookInKey, lookInVal := true, true
			for _, v := range val.Keys() {
				if lookInKey {
					if extractBlobRefsInternal(v, brs) {
						mayContain = true
					} else {
						// No need to look in the keys anymore.
						lookInKey = false
					}
				}
				if lookInVal {
					if extractBlobRefsInternal(val.MapIndex(v), brs) {
						mayContain = true
					} else {
						// No need to look in values anymore.
						lookInVal = false
					}
				}
				if !lookInKey && !lookInVal {
					// Look no further, no blob refs in the rest.
					break
				}
			}

		case vdl.Set:
			for _, v := range val.Keys() {
				if extractBlobRefsInternal(v, brs) {
					mayContain = true
				} else {
					// Look no further, no blob refs in the rest.
					break
				}
			}

		case vdl.Union:
			_, val = val.UnionField()
			extractBlobRefsInternal(val, brs)
			mayContain = true

		case vdl.Any, vdl.Optional:
			extractBlobRefsInternal(val.Elem(), brs)
			mayContain = true
		}
	}

	return mayContain
}
