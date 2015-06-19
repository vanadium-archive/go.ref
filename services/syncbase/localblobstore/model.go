// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package localblobstore is the interface to a local blob store.
// Implementations include fs_cablobstore.
package localblobstore

import "v.io/v23/context"

// A BlobStore represents a simple, content-addressable store.
type BlobStore interface {
	// NewBlobReader() returns a pointer to a newly allocated BlobReader on
	// the specified blobName.  BlobReaders should not be used concurrently
	// by multiple threads.  Returned handles should be closed with
	// Close().
	NewBlobReader(ctx *context.T, blobName string) (br BlobReader, err error)

	// NewBlobWriter() returns a pointer to a newly allocated BlobWriter on
	// a newly created blob name, which can be found using the Name()
	// method.  BlobWriters should not be used concurrently by multiple
	// threads.  The returned handle should be closed with either the
	// Close() or CloseWithoutFinalize() method to avoid leaking file
	// handles.
	NewBlobWriter(ctx *context.T) (bw BlobWriter, err error)

	// ResumeBlobWriter() returns a pointer to a newly allocated BlobWriter on
	// an old, but unfinalized blob name.
	ResumeBlobWriter(ctx *context.T, blobName string) (bw BlobWriter, err error)

	// DeleteBlob() deletes the named blob from the BlobStore.
	DeleteBlob(ctx *context.T, blobName string) (err error)

	// GC() removes old temp files and content-addressed blocks that are no
	// longer referenced by any blob.  It may be called concurrently with
	// other calls to GC(), and with uses of BlobReaders and BlobWriters.
	GC(ctx *context.T) error

	// ListBlobIds() returns an iterator that can be used to enumerate the
	// blobs in a BlobStore.  Expected use is:
	//
	//	iter := bs.ListBlobIds(ctx)
	//	for iter.Advance() {
	//	  // Process iter.Value() here.
	//	}
	//	if iter.Err() != nil {
	//	  // The loop terminated early due to an error.
	//	}
	ListBlobIds(ctx *context.T) (iter Iter)

	// ListCAIds() returns an iterator that can be used to enumerate the
	// content-addressable fragments in a BlobStore.  Expected use is:
	//
	//	iter := bs.ListCAIds(ctx)
	//	for iter.Advance() {
	//	  // Process iter.Value() here.
	//	}
	//	if iter.Err() != nil {
	//	  // The loop terminated early due to an error.
	//	}
	ListCAIds(ctx *context.T) (iter Iter)

	// Root() returns the name of the root directory where the BlobStore is stored.
	Root() string
}

// A BlobReader allows a blob to be read using the standard ReadAt(), Read(),
// and Seek() calls.  A BlobReader can be created with NewBlobReader(), and
// should be closed with the Close() method to avoid leaking file handles.
type BlobReader interface {
	// ReadAt() fills b[] with up to len(b) bytes of data starting at
	// position "at" within the blob that the BlobReader indicates, and
	// returns the number of bytes read.
	ReadAt(b []byte, at int64) (n int, err error)

	// Read() fills b[] with up to len(b) bytes of data starting at the
	// current seek position of the BlobReader within the blob that the
	// BlobReader indicates, and then both returns the number of bytes read
	// and advances the BlobReader's seek position by that amount.
	Read(b []byte) (n int, err error)

	// Seek() sets the seek position of the BlobReader to offset if
	// whence==0, offset+current_seek_position if whence==1, and
	// offset+end_of_blob if whence==2, and then returns the current seek
	// position.
	Seek(offset int64, whence int) (result int64, err error)

	// Close() indicates that the client will perform no further operations
	// on the BlobReader.  It releases any resources held by the
	// BlobReader.
	Close() error

	// Name() returns the BlobReader's name.
	Name() string

	// Size() returns the BlobReader's size.
	Size() int64

	// IsFinalized() returns whether the BlobReader has been finalized.
	IsFinalized() bool

	// Hash() returns the BlobReader's hash.  It may be nil if the blob is
	// not finalized.
	Hash() []byte
}

// A BlockOrFile represents a vector of bytes, and contains either a data
// block (as a []byte), or a (file name, size, offset) triple.
type BlockOrFile struct {
	Block    []byte // If FileName is empty, the bytes represented.
	FileName string // If non-empty, the name of the file containing the bytes.
	Size     int64  // If FileName is non-empty, the number of bytes (or -1 for "all")
	Offset   int64  // If FileName is non-empty, the offset of the relevant bytes within the file.
}

// A BlobWriter allows a blob to be written.  If a blob has not yet been
// finalized, it also allows that blob to be extended.  A BlobWriter may be
// created with NewBlobWriter(), and should be closed with Close() or
// CloseWithoutFinalize().
type BlobWriter interface {
	// AppendBlob() adds a (substring of a) pre-existing blob to the blob
	// being written by the BlobWriter.  The fragments of the pre-existing
	// blob are not physically copied; they are referenced by both blobs.
	AppendBlob(blobName string, size int64, offset int64) (err error)

	// AppendFragment() appends a fragment to the blob being written by the
	// BlobWriter, where the fragment is composed of the byte vectors
	// described by the elements of item[].  The fragment is copied into
	// the blob store.
	AppendFragment(item ...BlockOrFile) (err error)

	// Close() finalizes the BlobWriter, and indicates that the client will
	// perform no further append operations on the BlobWriter.  Any
	// internal open file handles are closed.
	Close() (err error)

	// CloseWithoutFinalize() indicates that the client will perform no
	// further append operations on the BlobWriter, but does not finalize
	// the blob.  Any internal open file handles are closed.  Clients are
	// expected to need this operation infrequently.
	CloseWithoutFinalize() (err error)

	// Name() returns the BlobWriter's name.
	Name() string

	// Size() returns the BlobWriter's size.
	Size() int64

	// IsFinalized() returns whether the BlobWriter has been finalized.
	IsFinalized() bool

	// Hash() returns the BlobWriter's hash, reflecting the bytes written so far.
	Hash() []byte
}

// A Iter represents an iterator that allows the client to enumerate
// all the blobs of fragments in a BlobStore.
type Iter interface {
	// Advance() stages an item so that it may be retrieved via Value.
	// Returns true iff there is an item to retrieve.  Advance must be
	// called before Value is called.
	Advance() (advanced bool)

	// Value() returns the item that was staged by Advance.  May panic if
	// Advance returned false or was not called.  Never blocks.
	Value() (name string)

	// Err() returns any error encountered by Advance.  Never blocks.
	Err() error
}
