// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A test for chunkmap.
package chunkmap_test

import "bytes"
import "io/ioutil"
import "math/rand"
import "os"
import "runtime"
import "testing"

import "v.io/syncbase/x/ref/services/syncbase/localblobstore/chunkmap"
import "v.io/v23/context"

import "v.io/x/ref/test"
import _ "v.io/x/ref/runtime/factories/generic"

// id() returns a new random 16-byte byte vector.
func id() []byte {
	v := make([]byte, 16)
	for i := 0; i != len(v); i++ {
		v[i] = byte(rand.Int31n(256))
	}
	return v
}

// verifyBlobs() tests that the blobs in *cm are those in b[], as revealed via
// the BlobStream() interface.
func verifyBlobs(t *testing.T, ctx *context.T, cm *chunkmap.ChunkMap, b [][]byte) {
	_, _, callerLine, _ := runtime.Caller(1)
	seen := make([]bool, len(b)) // seen[i] == whether b[i] seen in *cm
	bs := cm.NewBlobStream(ctx)
	var i int
	for i = 0; bs.Advance(); i++ {
		blob := bs.Value(nil)
		var j int
		for j = 0; j != len(b) && bytes.Compare(b[j], blob) != 0; j++ {
		}
		if j == len(b) {
			t.Errorf("chunkmap_test: line %d: unexpected blob %v present in ChunkMap",
				callerLine, blob)
		} else if seen[j] {
			t.Errorf("chunkmap_test: line %d: blob %v seen twice in ChunkMap",
				callerLine, blob)
		} else {
			seen[j] = true
		}
	}
	if i != len(b) {
		t.Errorf("chunkmap_test: line %d: found %d blobs in ChunkMap, but expected %d",
			callerLine, i, len(b))
	}
	for j := range seen {
		if !seen[j] {
			t.Errorf("chunkmap_test: line %d: blob %v not seen un ChunkMap",
				callerLine, b[j])
		}
	}
	if bs.Err() != nil {
		t.Errorf("chunkmap_test: line %d: BlobStream.Advance: unexpected error %v",
			callerLine, bs.Err())
	}
}

// verifyNoChunksInBlob() tests that blob b[blobi] has no chunks in *cm, as
// revealed by the ChunkStream interface.
func verifyNoChunksInBlob(t *testing.T, ctx *context.T, cm *chunkmap.ChunkMap, blobi int, b [][]byte) {
	_, _, callerLine, _ := runtime.Caller(1)
	cs := cm.NewChunkStream(ctx, b[blobi])
	for i := 0; cs.Advance(); i++ {
		t.Errorf("chunkmap_test: line %d: blob %d: chunk %d: %v",
			callerLine, blobi, i, cs.Value(nil))
	}
	if cs.Err() != nil {
		t.Errorf("chunkmap_test: line %d: blob %d: ChunkStream.Advance: unexpected error %v",
			callerLine, blobi, cs.Err())
	}
}

// verifyChunksInBlob() tests that blob b[blobi] in *cm contains the expected
// chunks from c[].  Each blob is expected to have 8 chunks, 0...7, except that
// b[1] has c[8] instead of c[4] for chunk 4.
func verifyChunksInBlob(t *testing.T, ctx *context.T, cm *chunkmap.ChunkMap, blobi int, b [][]byte, c [][]byte) {
	_, _, callerLine, _ := runtime.Caller(1)
	var err error
	var i int
	cs := cm.NewChunkStream(ctx, b[blobi])
	for i = 0; cs.Advance(); i++ {
		chunk := cs.Value(nil)
		chunki := i
		if blobi == 1 && i == 4 { // In blob 1, c[4] is replaced by c[8]
			chunki = 8
		}
		if bytes.Compare(c[chunki], chunk) != 0 {
			t.Errorf("chunkmap_test: line %d: blob %d: chunk %d: got %v, expected %v",
				callerLine, blobi, i, chunk, c[chunki])
		}

		var loc chunkmap.Location
		loc, err = cm.LookupChunk(ctx, chunk)
		if err != nil {
			t.Errorf("chunkmap_test: line %d: blob %d: chunk %d: LookupChunk got unexpected error: %v",
				callerLine, blobi, i, err)
		} else {
			if i == 4 {
				if bytes.Compare(loc.BlobID, b[blobi]) != 0 {
					t.Errorf("chunkmap_test: line %d: blob %d: chunk %d: Location.BlobID got %v, expected %v",
						callerLine, blobi, i, loc.BlobID, b[blobi])
				}
			} else {
				if bytes.Compare(loc.BlobID, b[0]) != 0 && bytes.Compare(loc.BlobID, b[1]) != 0 {
					t.Errorf("chunkmap_test: line %d: blob %d: chunk %d: Location.BlobID got %v, expected %v",
						callerLine, blobi, i, loc.BlobID, b[blobi])
				}
			}
			if loc.Offset != int64(i) {
				t.Errorf("chunkmap_test: line %d: blob %d: chunk %d: Location.Offset got %d, expected %d",
					callerLine, blobi, i, loc.Offset, i)
			}
			if loc.Size != 1 {
				t.Errorf("chunkmap_test: line %d: blob %d: chunk %d: Location.Size got %d, expected 1",
					callerLine, blobi, i, loc.Size)
			}

			// The offsets and sizes will match, between the result
			// from the stream and the result from LookupChunk(),
			// because for all chunks written to both, they are
			// written to the same places.  However, the blob need
			// not match, since LookupChunk() will return an
			// arbitrary Location in the store that contains the
			// chunk.
			loc2 := cs.Location()
			if loc.Offset != loc2.Offset || loc.Size != loc2.Size {
				t.Errorf("chunkmap_test: line %d: blob %d: chunk %d: disagreement about location: LookupChunk %v vs ChunkStream %v",
					callerLine, blobi, i, loc, loc2)
			}
		}
	}
	if cs.Err() != nil {
		t.Errorf("chunkmap_test: line %d: blob %d: ChunkStream.Err() unepxected error %v",
			callerLine, blobi, cs.Err())
	}
	if i != 8 {
		t.Errorf("chunkmap_test: line %d: blob %d: ChunkStream.Advance unexpectedly saw %d chunks, expected 8",
			callerLine, blobi, i)
	}
}

// TestAddRetrieveAndDelete() tests insertion, retrieval, and deletion of blobs
// from a ChunkMap.  It's all done in one test case, because one cannot retrieve
// or delete blobs that have not been inserted.
func TestAddRetrieveAndDelete(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	// Make a temporary directory.
	var err error
	var testDirName string
	testDirName, err = ioutil.TempDir("", "chunkmap_test")
	if err != nil {
		t.Fatalf("chunkmap_test: can't make tmp directory: %v", err)
	}
	defer os.RemoveAll(testDirName)

	// Create a chunkmap.
	var cm *chunkmap.ChunkMap
	cm, err = chunkmap.New(ctx, testDirName)
	if err != nil {
		t.Fatalf("chunkmap_test: chunkmap.New failed: %v", err)
	}

	// Two blobs: b[0] and b[1].
	b := [][]byte{id(), id()}

	// Nine chunks: c[0 .. 8]
	c := [][]byte{id(), id(), id(), id(), id(), id(), id(), id(), id()}

	// Verify that there are no blobs, or chunks in blobs initially.
	verifyBlobs(t, ctx, cm, nil)
	verifyNoChunksInBlob(t, ctx, cm, 0, b)
	verifyNoChunksInBlob(t, ctx, cm, 1, b)

	// Verify that all chunks have no locations initially.
	for chunki := range c {
		_, err = cm.LookupChunk(ctx, c[chunki])
		if err == nil {
			t.Errorf("chunkmap_test: chunk %d: LookupChunk: unexpected lack of error", chunki)
		}
	}

	// Put chunks 0..7 into blob 0, and chunks 0..3, 8, 5..7 into blob 1.
	// Each blob is treated as size 1.
	for blobi := 0; blobi != 2; blobi++ {
		for i := 0; i != 8; i++ {
			chunki := i
			if blobi == 1 && i == 4 { // In blob 1, c[4] 4 is replaced by c[8]
				chunki = 8
			}
			err = cm.AssociateChunkWithLocation(ctx, c[chunki],
				chunkmap.Location{BlobID: b[blobi], Offset: int64(i), Size: 1})
			if err != nil {
				t.Errorf("chunkmap_test: blob %d: AssociateChunkWithLocation: unexpected error: %v",
					blobi, err)
			}
		}
	}

	// Verify that the blobs are present, with the chunks specified.
	verifyBlobs(t, ctx, cm, b)
	verifyChunksInBlob(t, ctx, cm, 0, b, c)
	verifyChunksInBlob(t, ctx, cm, 1, b, c)

	// Verify that all chunks now have locations.
	for chunki := range c {
		_, err = cm.LookupChunk(ctx, c[chunki])
		if err != nil {
			t.Errorf("chunkmap_test: chunk %d: LookupChunk: unexpected error: %v",
				chunki, err)
		}
	}

	// Delete b[0].
	err = cm.DeleteBlob(ctx, b[0])
	if err != nil {
		t.Errorf("chunkmap_test: blob 0: DeleteBlob: unexpected error: %v", err)
	}

	// Verify that all chunks except chunk 4 (which was in only blob 0)
	// still have locations.
	for chunki := range c {
		_, err = cm.LookupChunk(ctx, c[chunki])
		if chunki == 4 {
			if err == nil {
				t.Errorf("chunkmap_test: chunk %d: LookupChunk: expected lack of error",
					chunki)
			}
		} else {
			if err != nil {
				t.Errorf("chunkmap_test: chunk %d: LookupChunk: unexpected error: %v",
					chunki, err)
			}
		}
	}

	// Verify that blob 0 is gone, but blob 1 remains.
	verifyBlobs(t, ctx, cm, b[1:])
	verifyNoChunksInBlob(t, ctx, cm, 0, b)
	verifyChunksInBlob(t, ctx, cm, 1, b, c)

	// Delete b[1].
	err = cm.DeleteBlob(ctx, b[1])
	if err != nil {
		t.Errorf("chunkmap_test: blob 1: DeleteBlob: unexpected error: %v",
			err)
	}

	// Verify that there are no blobs, or chunks in blobs once more.
	verifyBlobs(t, ctx, cm, nil)
	verifyNoChunksInBlob(t, ctx, cm, 0, b)
	verifyNoChunksInBlob(t, ctx, cm, 1, b)

	// Verify that all chunks have no locations once more.
	for chunki := range c {
		_, err = cm.LookupChunk(ctx, c[chunki])
		if err == nil {
			t.Errorf("chunkmap_test: chunk %d: LookupChunk: unexpected lack of error",
				chunki)
		}
	}

	err = cm.Close()
	if err != nil {
		t.Errorf("chunkmap_test: unexpected error closing ChunkMap: %v", err)
	}
}
