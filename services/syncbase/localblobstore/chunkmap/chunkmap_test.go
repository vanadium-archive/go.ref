// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A test for chunkmap.
package chunkmap_test

import "bytes"
import "io/ioutil"
import "math/rand"
import "os"
import "testing"

import "v.io/syncbase/x/ref/services/syncbase/localblobstore/chunkmap"
import "v.io/v23/context"

// import "v.io/v23/verror"
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

// verifyNoBlob() tests that blob b[blobi] is not present in *cm.
// callSite is a callsite identifier, output in all error messages.
func verifyNoBlob(t *testing.T, ctx *context.T, cm *chunkmap.ChunkMap, blobi int, b [][]byte, callSite int) {
	bs := cm.NewBlobStream(ctx, b[blobi])
	for i := 0; bs.Advance(); i++ {
		t.Errorf("chunkmap_test: callsite %d: blob %d: chunk %d: %v",
			callSite, blobi, i, bs.Value(nil))
	}
	if bs.Err() != nil {
		t.Errorf("chunkmap_test: callsite %d: blob %d: BlobStream.Advance: unexpected error %v",
			callSite, blobi, bs.Err())
	}
}

// verifyBlob() tests that blob b[blobi] in *cm contains the expected chunks from c[].
// Each blob is expected to have 8 chunks, 0...7, except that b[1] has c[8] instead of c[4] for chunk 4.
// callSite is a callsite identifier, output in all error messages.
func verifyBlob(t *testing.T, ctx *context.T, cm *chunkmap.ChunkMap, blobi int, b [][]byte, c [][]byte, callSite int) {
	var err error
	var i int
	bs := cm.NewBlobStream(ctx, b[blobi])
	for i = 0; bs.Advance(); i++ {
		chunk := bs.Value(nil)
		chunki := i
		if blobi == 1 && i == 4 { // In blob 1, c[4] is replaced by c[8]
			chunki = 8
		}
		if bytes.Compare(c[chunki], chunk) != 0 {
			t.Errorf("chunkmap_test: callsite %d: blob %d: chunk %d: got %v, expected %v",
				callSite, blobi, i, chunk, c[chunki])
		}

		var loc chunkmap.Location
		loc, err = cm.LookupChunk(ctx, chunk)
		if err != nil {
			t.Errorf("chunkmap_test: callsite %d: blob %d: chunk %d: LookupChunk got unexpected error: %v",
				callSite, blobi, i, err)
		} else {
			if i == 4 {
				if bytes.Compare(loc.Blob, b[blobi]) != 0 {
					t.Errorf("chunkmap_test: callsite %d: blob %d: chunk %d: Location.Blob got %v, expected %v",
						callSite, blobi, i, loc.Blob, b[blobi])
				}
			} else {
				if bytes.Compare(loc.Blob, b[0]) != 0 && bytes.Compare(loc.Blob, b[1]) != 0 {
					t.Errorf("chunkmap_test: callsite %d: blob %d: chunk %d: Location.Blob got %v, expected %v",
						callSite, blobi, i, loc.Blob, b[blobi])
				}
			}
			if loc.Offset != int64(i) {
				t.Errorf("chunkmap_test: callsite %d: blob %d: chunk %d: Location.Offset got %d, expected %d",
					callSite, blobi, i, loc.Offset, i)
			}
			if loc.Size != 1 {
				t.Errorf("chunkmap_test: callsite %d: blob %d: chunk %d: Location.Size got %d, expected 1",
					callSite, blobi, i, loc.Size)
			}

			loc2 := bs.Location()
			if bytes.Compare(loc.Blob, loc.Blob) != 0 || loc.Offset != loc2.Offset || loc.Size != loc2.Size {
				t.Errorf("chunkmap_test: callsite %d: blob %d: chunk %d: disagreement about location: LookupChunk %v vs BlobStream %v",
					callSite, blobi, i, loc, loc2)
			}
		}
	}
	if bs.Err() != nil {
		t.Errorf("chunkmap_test: callsite %d: blob %d: BlobStream.Err() unepxected error %v",
			callSite, blobi, bs.Err())
	}
	if i != 8 {
		t.Errorf("chunkmap_test: callsite %d: blob %d: BlobStream.Advance unexpectedly saw %d chunks, expected 8",
			callSite, blobi, i)
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

	// Verify that there are no chunks in blobs initially.
	verifyNoBlob(t, ctx, cm, 0, b, 0)
	verifyNoBlob(t, ctx, cm, 1, b, 1)

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
				chunkmap.Location{Blob: b[blobi], Offset: int64(i), Size: 1})
			if err != nil {
				t.Errorf("chunkmap_test: blob %d: AssociateChunkWithLocation: unexpected error: %v",
					blobi, err)
			}
		}
	}

	// Verify that the blobs contain the chunks specified.
	verifyBlob(t, ctx, cm, 0, b, c, 2)
	verifyBlob(t, ctx, cm, 1, b, c, 3)

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
	verifyNoBlob(t, ctx, cm, 0, b, 4)
	verifyBlob(t, ctx, cm, 1, b, c, 5)

	// Delete b[1].
	err = cm.DeleteBlob(ctx, b[1])
	if err != nil {
		t.Errorf("chunkmap_test: blob 1: DeleteBlob: unexpected error: %v",
			err)
	}

	// Verify that there are no chunks in blobs initially.
	verifyNoBlob(t, ctx, cm, 0, b, 6)
	verifyNoBlob(t, ctx, cm, 1, b, 7)

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
