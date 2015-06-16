// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A test for fs_cablobstore
package fs_cablobstore_test

import "bytes"
import "crypto/md5"
import "fmt"
import "io"
import "io/ioutil"
import "os"
import "path/filepath"
import "testing"
import "v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore"
import "v.io/v23/context"
import "v.io/v23/verror"
import "v.io/x/ref/test"
import _ "v.io/x/ref/runtime/factories/generic"

func TestCreate(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	// Make a temporary directory.
	var err error
	var testDirName string
	testDirName, err = ioutil.TempDir("", "fs_cablobstore_test")
	if err != nil {
		t.Fatalf("fs_cablobstore_test: can't make tmp directory: %v\n", err)
	}
	defer os.RemoveAll(testDirName)

	// Check that we can create an fs_cablobstore.
	var fscabs *fs_cablobstore.FsCaBlobStore
	fscabs, err = fs_cablobstore.Create(ctx, testDirName)
	if err != nil {
		t.Errorf("fs_cablobstore.Create failed: %v", err)
	}

	// Check that there are no files in the newly-created tree.
	iterator := fscabs.ListBlobIds(ctx)
	for iterator.Advance() {
		fileName := iterator.Value()
		t.Errorf("unexpected file %q\n", fileName)
	}
	if iterator.Err() != nil {
		t.Errorf("fs_cablobstore.ListBlobIds iteration failed: %v", iterator.Err())
	}
}

// A blobOrBlockOrFile represents some bytes that may be contained in a named
// blob, a named file, or in an explicit slice of bytes.
type blobOrBlockOrFile struct {
	blob   string // If non-emtpy, the name of the blob containing the bytes.
	file   string // If non-empty and blob is empty, the name of the file containing the bytes.
	size   int64  // Size of part of file or blob, or -1 for "everything until EOF".
	offset int64  // Offset within file or blob.
	block  []byte // If both blob and file are empty, a slice containing the bytes.
}

// A testBlob records that some specified content has been stored with a given
// blob name in the blob store.
type testBlob struct {
	content  []byte // content that has been stored.
	blobName string // the name of the blob.
}

// removeBlobFromBlobVector() removes the entry named blobName from
// blobVector[], returning the new vector.
func removeBlobFromBlobVector(blobVector []testBlob, blobName string) []testBlob {
	n := len(blobVector)
	i := 0
	for i = 0; i != n && blobName != blobVector[i].blobName; i++ {
	}
	if i != n {
		blobVector[i] = blobVector[n-1]
		blobVector = blobVector[0 : n-1]
	}
	return blobVector
}

// writeBlob() writes a new blob to *fscabs, and returns its name.  The new
// blob's content is described by the elements of data[].  Any error messages
// generated include the index of the blob in blobVector and its content; the
// latter is assumed to be printable.  The expected content of the the blob is
// "content", so that this routine can check it.  If useResume is true, and data[]
// has length more than 1, the function artificially uses ResumeBlobWriter(),
// to test it.
func writeBlob(t *testing.T, ctx *context.T, fscabs *fs_cablobstore.FsCaBlobStore, blobVector []testBlob,
	content []byte, useResume bool, data ...blobOrBlockOrFile) []testBlob {
	var bw *fs_cablobstore.BlobWriter
	var err error
	bw, err = fscabs.NewBlobWriter(ctx)
	if err != nil {
		t.Fatalf("fs_cablobstore.NewBlobWriter blob %d:%s failed: %v", len(blobVector), string(content), err)
	}
	blobName := bw.Name()

	// Construct the blob from the pieces.
	// There is a loop within the loop to exercise the possibility of
	// passing multiple fragments to AppendFragment().
	for i := 0; i != len(data) && err == nil; {
		if len(data[i].blob) != 0 {
			err = bw.AppendBlob(data[i].blob, data[i].size, data[i].offset)
			if err != nil {
				t.Errorf("fs_cablobstore.AppendBlob %d:%s blob %s failed: %v", len(blobVector), string(content), data[i].blob, err)
			}
			i++
		} else {
			var pieces []fs_cablobstore.BlockOrFile
			for ; i != len(data) && len(data[i].blob) == 0; i++ {
				if len(data[i].file) != 0 {
					pieces = append(pieces, fs_cablobstore.BlockOrFile{
						FileName: data[i].file,
						Size:     data[i].size,
						Offset:   data[i].offset})
				} else {
					pieces = append(pieces, fs_cablobstore.BlockOrFile{Block: data[i].block})
				}
			}
			err = bw.AppendFragment(pieces...)
			if err != nil {
				t.Errorf("fs_cablobstore.AppendFragment %d:%s failed on %v: %v", len(blobVector), string(content), pieces, err)
			}
		}
		if useResume && i < len(data)-1 && err == nil {
			err = bw.CloseWithoutFinalize()
			if err == nil {
				bw, err = fscabs.ResumeBlobWriter(ctx, blobName)
			}
		}
	}

	if bw != nil {
		if bw.Size() != int64(len(content)) {
			t.Errorf("fs_cablobstore.Size before finalization %d:%s got %d, expected %d", len(blobVector), string(content), bw.Size(), len(content))
		}
		if bw.IsFinalized() {
			t.Errorf("fs_cablobstore.IsFinalized %d:%s got true, expected false", len(blobVector), string(content))
		}
		err = bw.Close()
		if err != nil {
			t.Errorf("fs_cablobstore.Close %d:%s failed: %v", len(blobVector), string(content), err)
		}
		if !bw.IsFinalized() {
			t.Errorf("fs_cablobstore.IsFinalized %d:%s got true, expected false", len(blobVector), string(content))
		}
		if bw.Size() != int64(len(content)) {
			t.Errorf("fs_cablobstore.Size %d:%s after finalization got %d, expected %d", len(blobVector), string(content), bw.Size(), len(content))
		}
		if bw.Name() != blobName {
			t.Errorf("fs_cablobstore %d:%s name changed when finalized was %s now %s", len(blobVector), string(content), blobName, bw.Name())
		}
		hasher := md5.New()
		hasher.Write(content)
		if bytes.Compare(bw.Hash(), hasher.Sum(nil)) != 0 {
			t.Errorf("fs_cablobstore %d:%s BlobWriter.Hash got %v, expected %v", len(blobVector), string(content), bw.Hash(), hasher.Sum(nil))
		}
	}

	return append(blobVector,
		testBlob{
			content:  content,
			blobName: blobName,
		})
}

// readBlob() returns a substring of the content of the blob named blobName in *fscabs.
// The return values are:
// - the "size" bytes from the content, starting at the given "offset",
//   measured from "whence" (as defined by io.Seeker.Seek).
// - the position to which BlobBeader seeks to,
// - the md5 hash of the bytes read, and
// - the md5 hash of the bytes of the blob, as returned by BlobReader.Hash(),
// - and error.
func readBlob(ctx *context.T, fscabs *fs_cablobstore.FsCaBlobStore, blobName string,
	size int64, offset int64, whence int) (content []byte, pos int64, hash []byte, fullHash []byte, err error) {

	var br *fs_cablobstore.BlobReader
	hasher := md5.New()
	br, err = fscabs.NewBlobReader(ctx, blobName)
	if err == nil {
		buf := make([]byte, 8192, 8192)
		fullHash = br.Hash()
		pos, err = br.Seek(offset, whence)
		if err == nil {
			var n int
			first := true // Read at least once, to test reading zero bytes.
			for err == nil && (size == -1 || int64(len(content)) < size || first) {
				// Read just what was asked for.
				var toRead []byte = buf
				if size >= 0 && int(size)-len(content) < len(buf) {
					toRead = buf[0 : int(size)-len(content)]
				}
				n, err = br.Read(toRead)
				hasher.Write(toRead[0:n])
				if size >= 0 && int64(len(content)+n) > size {
					n = int(size) - len(content)
				}
				content = append(content, toRead[0:n]...)
				first = false
			}
		}
		br.Close()
	}
	return content, pos, hasher.Sum(nil), fullHash, err
}

// checkWrittenBlobsAreReadable() checks that the blobs in blobVector[] can be
// read, and that they contain the appropriate data.
func checkWrittenBlobsAreReadable(t *testing.T, ctx *context.T, fscabs *fs_cablobstore.FsCaBlobStore, blobVector []testBlob) {
	for i := range blobVector {
		var size int64
		data := blobVector[i].content
		dataLen := int64(len(data))
		blobName := blobVector[i].blobName
		for size = -1; size != dataLen+1; size++ {
			var offset int64
			for offset = -dataLen - 1; offset != dataLen+1; offset++ {
				for whence := -1; whence != 4; whence++ {
					content, pos, hash, fullHash, err := readBlob(ctx, fscabs, blobName, size, offset, whence)

					// Compute expected seek position.
					expectedPos := offset
					if whence == 2 {
						expectedPos += dataLen
					}

					// Computed expected size.
					expectedSize := size
					if expectedSize == -1 || expectedPos+expectedSize > dataLen {
						expectedSize = dataLen - expectedPos
					}

					// Check that reads behave as expected.
					if (whence == -1 || whence == 3) &&
						verror.ErrorID(err) == "v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore.errBadSeekWhence" {
						// Expected error from bad "whence" value.
					} else if expectedPos < 0 &&
						verror.ErrorID(err) == "v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore.errNegativeSeekPosition" {
						// Expected error from negative Seek position.
					} else if expectedPos > dataLen &&
						verror.ErrorID(err) == "v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore.errIllegalPositionForRead" {
						// Expected error from too high a Seek position.
					} else if 0 <= expectedPos && expectedPos+expectedSize <= int64(len(data)) &&
						bytes.Compare(data[expectedPos:expectedPos+expectedSize], content) == 0 && err == io.EOF &&
						pos == expectedPos && expectedPos+expectedSize == dataLen {
						// Expected success with EOF.
					} else if 0 <= expectedPos && expectedPos+expectedSize <= int64(len(data)) &&
						bytes.Compare(data[expectedPos:expectedPos+expectedSize], content) == 0 && err == nil &&
						pos == expectedPos && expectedPos+expectedSize != dataLen {
						if pos == 0 && size == -1 && bytes.Compare(hash, fullHash) != 0 {
							t.Errorf("fs_cablobstore read test on %q size %d offset %d whence %d; got hash %v, expected %v  (blob is %q)",
								string(data), size, offset, whence,
								hash, fullHash, blobName)
						} // Else expected success without EOF.
					} else {
						t.Errorf("fs_cablobstore read test on %q size %d offset %d whence %d yields %q pos %d %v   (blob is %q)",
							string(data), size, offset, whence,
							content, pos, err, blobName)
					}
				}
			}
		}
	}
}

// checkAllBlobs() checks all the blobs in *fscabs to ensure they correspond to
// those in blobVector[].
func checkAllBlobs(t *testing.T, ctx *context.T, fscabs *fs_cablobstore.FsCaBlobStore, blobVector []testBlob, testDirName string) {
	blobCount := 0
	iterator := fscabs.ListBlobIds(ctx)
	for iterator.Advance() {
		fileName := iterator.Value()
		i := 0
		for ; i != len(blobVector) && fileName != blobVector[i].blobName; i++ {
		}
		if i == len(blobVector) {
			t.Errorf("fs_cablobstore.ListBlobIds found unexpected file %s", fileName)
		} else {
			content, pos, hash, fullHash, err := readBlob(ctx, fscabs, fileName, -1, 0, 0)
			if err != nil && err != io.EOF {
				t.Errorf("fs_cablobstore.ListCAIds can't read %q: %v", filepath.Join(testDirName, fileName), err)
			} else if bytes.Compare(blobVector[i].content, content) != 0 {
				t.Errorf("fs_cablobstore.ListCAIds found unexpected blob content: %q, contains %q, expected %q",
					filepath.Join(testDirName, fileName), content, string(blobVector[i].content))
			} else if pos != 0 {
				t.Errorf("fs_cablobstore.ListCAIds Seek on %q returned %d instead of 0",
					filepath.Join(testDirName, fileName), pos)
			}
			if bytes.Compare(hash, fullHash) != 0 {
				t.Errorf("fs_cablobstore.ListCAIds read on %q; got hash %v, expected %v",
					fileName, hash, fullHash)
			}
		}
		blobCount++
	}
	if iterator.Err() != nil {
		t.Errorf("fs_cablobstore.ListBlobIds iteration failed: %v", iterator.Err())
	}
	if blobCount != len(blobVector) {
		t.Errorf("fs_cablobstore.ListBlobIds iteration expected 4 files, got %d", blobCount)
	}
}

// checkFragments() checks all the fragments in *fscabs to ensure they
// correspond to those fragmentMap[].
func checkFragments(t *testing.T, ctx *context.T, fscabs *fs_cablobstore.FsCaBlobStore, fragmentMap map[string]bool, testDirName string) {
	caCount := 0
	iterator := fscabs.ListCAIds(ctx)
	for iterator.Advance() {
		fileName := iterator.Value()
		content, err := ioutil.ReadFile(filepath.Join(testDirName, fileName))
		if err != nil && err != io.EOF {
			t.Errorf("fs_cablobstore.ListCAIds can't read %q: %v", filepath.Join(testDirName, fileName), err)
		} else if !fragmentMap[string(content)] {
			t.Errorf("fs_cablobstore.ListCAIds found unexpected fragment entry: %q, contains %q", filepath.Join(testDirName, fileName), content)
		} else {
			hasher := md5.New()
			hasher.Write(content)
			hash := hasher.Sum(nil)
			nameFromContent := filepath.Join("cas",
				fmt.Sprintf("%02x", hash[0]),
				fmt.Sprintf("%02x", hash[1]),
				fmt.Sprintf("%02x", hash[2]),
				fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
					hash[3],
					hash[4], hash[5], hash[6], hash[7],
					hash[8], hash[9], hash[10], hash[11],
					hash[12], hash[13], hash[14], hash[15]))
			if nameFromContent != fileName {
				t.Errorf("fs_cablobstore.ListCAIds hash of fragment: got %q, expected %q (content=%s)", nameFromContent, fileName, string(content))
			}
		}
		caCount++
	}
	if iterator.Err() != nil {
		t.Errorf("fs_cablobstore.ListCAIds iteration failed: %v", iterator.Err())
	}
	if caCount != len(fragmentMap) {
		t.Errorf("fs_cablobstore.ListCAIds iteration expected %d files, got %d", len(fragmentMap), caCount)
	}
}

// This test case tests adding files, retrieving them and deleting them.  One
// can't retrieve or delete something that hasn't been created, so it's all one
// test case.
func TestAddRetrieveAndDelete(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	// Make a temporary directory.
	var err error
	var testDirName string
	testDirName, err = ioutil.TempDir("", "fs_cablobstore_test")
	if err != nil {
		t.Fatalf("fs_cablobstore_test: can't make tmp directory: %v\n", err)
	}
	defer os.RemoveAll(testDirName)

	// Create an fs_cablobstore.
	var fscabs *fs_cablobstore.FsCaBlobStore
	fscabs, err = fs_cablobstore.Create(ctx, testDirName)
	if err != nil {
		t.Fatalf("fs_cablobstore.Create failed: %v", err)
	}

	// Create the strings:  "wom", "bat", "wombat", "batwom", "atwo", "atwoatwoombatatwo".
	womData := []byte("wom")
	batData := []byte("bat")
	wombatData := []byte("wombat")
	batwomData := []byte("batwom")
	atwoData := []byte("atwo")
	atwoatwoombatatwoData := []byte("atwoatwoombatatwo")

	// fragmentMap will have an entry per content-addressed fragment.
	fragmentMap := make(map[string]bool)

	// Create the blobs, by various means.

	var blobVector []testBlob // Accumulate the blobs we create here.

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		womData, false,
		blobOrBlockOrFile{block: womData})
	womName := blobVector[len(blobVector)-1].blobName
	fragmentMap[string(womData)] = true

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		batData, false,
		blobOrBlockOrFile{block: batData})
	batName := blobVector[len(blobVector)-1].blobName
	fragmentMap[string(batData)] = true

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		wombatData, false,
		blobOrBlockOrFile{block: wombatData})
	firstWombatName := blobVector[len(blobVector)-1].blobName
	fragmentMap[string(wombatData)] = true

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		wombatData, true,
		blobOrBlockOrFile{block: womData},
		blobOrBlockOrFile{block: batData})

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		wombatData, false,
		blobOrBlockOrFile{
			blob:   firstWombatName,
			size:   -1,
			offset: 0})

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		wombatData, false,
		blobOrBlockOrFile{
			blob:   firstWombatName,
			size:   6,
			offset: 0})

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		batwomData, false,
		blobOrBlockOrFile{
			blob:   firstWombatName,
			size:   3,
			offset: 3},
		blobOrBlockOrFile{
			blob:   firstWombatName,
			size:   3,
			offset: 0})
	batwomName := blobVector[len(blobVector)-1].blobName

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		atwoData, false,
		blobOrBlockOrFile{
			blob:   batwomName,
			size:   4,
			offset: 1})
	atwoName := blobVector[len(blobVector)-1].blobName

	blobVector = writeBlob(t, ctx, fscabs, blobVector,
		atwoatwoombatatwoData, true,
		blobOrBlockOrFile{
			blob:   atwoName,
			size:   -1,
			offset: 0},
		blobOrBlockOrFile{
			blob:   atwoName,
			size:   4,
			offset: 0},
		blobOrBlockOrFile{
			blob:   firstWombatName,
			size:   -1,
			offset: 1},
		blobOrBlockOrFile{
			blob:   batName,
			size:   -1,
			offset: 1},
		blobOrBlockOrFile{
			blob:   womName,
			size:   2,
			offset: 0})
	atwoatwoombatatwoName := blobVector[len(blobVector)-1].blobName

	// -------------------------------------------------
	// Check that the state is as we expect.
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)

	// -------------------------------------------------
	// Nothing should change if we garbage collect.
	fscabs.GC(ctx)
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)

	// -------------------------------------------------
	// Ensure that deleting non-existent blobs fails.
	err = fscabs.DeleteBlob(ctx, "../../../../etc/passwd")
	if verror.ErrorID(err) != "v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore.errInvalidBlobName" {
		t.Errorf("DeleteBlob attempted to delete a bogus blob name")
	}
	err = fscabs.DeleteBlob(ctx, "foo/00/00/00/00000000000000000000000000")
	if verror.ErrorID(err) != "v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore.errInvalidBlobName" {
		t.Errorf("DeleteBlob attempted to delete a bogus blob name")
	}

	// -------------------------------------------------
	// Delete a blob.
	err = fscabs.DeleteBlob(ctx, batName)
	if err != nil {
		t.Errorf("DeleteBlob failed to delete blob %q: %v", batName, err)
	}
	blobVector = removeBlobFromBlobVector(blobVector, batName)

	// -------------------------------------------------
	// Check that the state is as we expect.
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)

	// -------------------------------------------------
	// Nothing should change if we garbage collect.
	fscabs.GC(ctx)
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)

	// -------------------------------------------------
	// Open a BlobReader on a blob we're about to delete,
	// so its fragments won't be garbage collected.

	var br *fs_cablobstore.BlobReader
	br, err = fscabs.NewBlobReader(ctx, atwoatwoombatatwoName)
	if err != nil {
		t.Errorf("NewBlobReader failed in blob %q: %v", atwoatwoombatatwoName, err)
	}

	// -------------------------------------------------
	// Delete a blob.  This should be the last on-disc reference to the
	// content-addressed fragment "bat", but the fragment won't be deleted
	// until close the reader and garbage collect.
	err = fscabs.DeleteBlob(ctx, atwoatwoombatatwoName)
	if err != nil {
		t.Errorf("DeleteBlob failed to delete blob %q: %v", atwoatwoombatatwoName, err)
	}
	blobVector = removeBlobFromBlobVector(blobVector, atwoatwoombatatwoName)

	// -------------------------------------------------
	// Check that the state is as we expect.
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)

	// -------------------------------------------------
	// Garbage collection should change nothing; the fragment involved
	// is still referenced from the open reader *br.
	fscabs.GC(ctx)
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)

	// -------------------------------------------------

	// Close the open BlobReader and garbage collect.
	err = br.Close()
	if err != nil {
		t.Errorf("BlobReader.Close failed on blob %q: %v", atwoatwoombatatwoName, err)
	}
	delete(fragmentMap, string(batData))

	fscabs.GC(ctx)
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)

	// -------------------------------------------------
	// Delete all blobs.
	for len(blobVector) != 0 {
		err = fscabs.DeleteBlob(ctx, blobVector[0].blobName)
		if err != nil {
			t.Errorf("DeleteBlob failed to delete blob %q: %v", blobVector[0].blobName, err)
		}
		blobVector = removeBlobFromBlobVector(blobVector, blobVector[0].blobName)
	}

	// -------------------------------------------------
	// Check that the state is as we expect.
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)

	// -------------------------------------------------
	// The remaining fragments should be removed when we garbage collect.
	for frag := range fragmentMap {
		delete(fragmentMap, frag)
	}
	fscabs.GC(ctx)
	checkWrittenBlobsAreReadable(t, ctx, fscabs, blobVector)
	checkAllBlobs(t, ctx, fscabs, blobVector, testDirName)
	checkFragments(t, ctx, fscabs, fragmentMap, testDirName)
}
