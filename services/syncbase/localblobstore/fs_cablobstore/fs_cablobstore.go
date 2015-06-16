// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fs_cablobstore implements a content addressable blob store
// on top of a file system.  It assumes that either os.Link() or
// os.Rename() is available.
package fs_cablobstore

// Internals:
//   The blobstore consists of a directory with "blob", "cas", and "tmp"
//   subdirectories.
//   - "tmp" is used for temporary files that are moved into place via
//     link()/unlink() or rename(), depending on what's available.
//   - "cas" contains files whose names are content hashes of the files being
//     named.  A few slashes are thrown into the name near the front so that no
//     single directory gets too large.
//   - "blob" contains files whose names are random numbers.  These names are
//     visible externally as "blob names".  Again, a few slashes are thrown
//     into the name near the front so that no single directory gets too large.
//     Each of these files contains a series of lines of the form:
//        d <size> <offset> <cas-fragment>
//     followed optionally by a line of the form:
//        f <md5-hash>
//     Each "d" line indicates that the next <size> bytes of the blob appear at
//     <offset> bytes into <cas-fragment>, which is in the "cas" subtree.  The
//     "f" line indicates that the blob is "finalized" and gives its complete
//     md5 hash.  No fragments may be appended to a finalized blob.

import "bufio"
import "crypto/md5"
import "fmt"
import "hash"
import "io"
import "io/ioutil"
import "math/rand"
import "os"
import "path/filepath"
import "strconv"
import "strings"
import "sync"
import "time"

import "v.io/v23/context"
import "v.io/v23/verror"

const pkgPath = "v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore"

var (
	errNotADir                = verror.Register(pkgPath+".errNotADir", verror.NoRetry, "{1:}{2:} Not a directory{:_}")
	errAppendFailed           = verror.Register(pkgPath+".errAppendFailed", verror.NoRetry, "{1:}{2:} fs_cablobstore.Append failed{:_}")
	errMalformedField         = verror.Register(pkgPath+".errMalformedField", verror.NoRetry, "{1:}{2:} Malformed field in blob specification{:_}")
	errAlreadyClosed          = verror.Register(pkgPath+".errAlreadyClosed", verror.NoRetry, "{1:}{2:} BlobWriter is already closed{:_}")
	errBlobAlreadyFinalized   = verror.Register(pkgPath+".errBlobAlreadyFinalized", verror.NoRetry, "{1:}{2:} Blob is already finalized{:_}")
	errIllegalPositionForRead = verror.Register(pkgPath+".errIllegalPositionForRead", verror.NoRetry, "{1:}{2:} BlobReader: illegal position {3} on Blob of size {4}{:_}")
	errBadSeekWhence          = verror.Register(pkgPath+".errBadSeekWhence", verror.NoRetry, "{1:}{2:} BlobReader: Bad value for 'whence' in Seek{:_}")
	errNegativeSeekPosition   = verror.Register(pkgPath+".errNegativeSeekPosition", verror.NoRetry, "{1:}{2:} BlobReader: negative position for Seek: offset {3}, whence {4}{:_}")
	errBadSizeOrOffset        = verror.Register(pkgPath+".errBadSizeOrOffset", verror.NoRetry, "{1:}{2:} Bad size ({3}) or offset ({4}) in blob {5} (size {6}){:_}")
	errMalformedBlobHash      = verror.Register(pkgPath+".errMalformedBlobHash", verror.NoRetry, "{1:}{2:} Blob {3} hash malformed hash{:_}")
	errInvalidBlobName        = verror.Register(pkgPath+".errInvalidBlobName", verror.NoRetry, "{1:}{2:} Invalid blob name {3}{:_}")
	errCantDeleteBlob         = verror.Register(pkgPath+".errCantDeleteBlob", verror.NoRetry, "{1:}{2:} Can't delete blob {3}{:_}")
	errBlobDeleted            = verror.Register(pkgPath+".errBlobDeleted", verror.NoRetry, "{1:}{2:} Blob is deleted{:_}")
	errSizeTooBigForFragment  = verror.Register(pkgPath+".errSizeTooBigForFragment", verror.NoRetry, "{1:}{2:} writing blob {1}, size too big for fragment{:1}")
)

// For the moment, we disallow others from accessing the tree where blobs are
// stored.  We could in the future relax this to 0711 or 0755.
const dirPermissions = 0700

// Subdirectories of the blobstore's tree
const (
	blobDir = "blob" // Subdirectory where blobs are indexed by blob id.
	casDir  = "cas"  // Subdirectory where blobs are indexed by content hash.
	tmpDir  = "tmp"  // Subdirectory where temporary files are created.
)

// An FsCaBlobStore represents a simple, content-addressable store.
type FsCaBlobStore struct {
	rootName string // The name of the root of the store.

	mu sync.Mutex // Protects fields below, plus
	// blobDesc.fragment, blobDesc.activeDescIndex,
	// and blobDesc.refCount.
	activeDesc []*blobDesc        // The blob descriptors in use by active BlobReaders and BlobWriters.
	toDelete   []*map[string]bool // Sets of items that active GC threads are about to delete. (Pointers to maps, to allow pointer comparison.)
}

// hashToFileName() returns the content-addressed name of the data with the
// specified hash, given its content hash.  Requires len(hash)==16.  An md5
// hash is suitable.
func hashToFileName(hash []byte) string {
	return filepath.Join(casDir,
		fmt.Sprintf("%02x", hash[0]),
		fmt.Sprintf("%02x", hash[1]),
		fmt.Sprintf("%02x", hash[2]),
		fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
			hash[3],
			hash[4], hash[5], hash[6], hash[7],
			hash[8], hash[9], hash[10], hash[11],
			hash[12], hash[13], hash[14], hash[15]))
}

// newBlobName() returns a new random name for a blob.
func newBlobName() string {
	return filepath.Join(blobDir,
		fmt.Sprintf("%02x", rand.Int31n(256)),
		fmt.Sprintf("%02x", rand.Int31n(256)),
		fmt.Sprintf("%02x", rand.Int31n(256)),
		fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
			rand.Int31n(256),
			rand.Int31n(256), rand.Int31n(256), rand.Int31n(256), rand.Int31n(256),
			rand.Int31n(256), rand.Int31n(256), rand.Int31n(256), rand.Int31n(256),
			rand.Int31n(256), rand.Int31n(256), rand.Int31n(256), rand.Int31n(256)))
}

// hashToString() returns a string representation of the hash.
// Requires len(hash)==16.  An md5 hash is suitable.
func hashToString(hash []byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
		hash[0], hash[1], hash[2], hash[3],
		hash[4], hash[5], hash[6], hash[7],
		hash[8], hash[9], hash[10], hash[11],
		hash[12], hash[13], hash[14], hash[15])
}

// stringToHash() converts a string in the format generated by hashToString()
// to a vector of 16 bytes.  If the string is malformed, the nil slice is
// returned.
func stringToHash(s string) []byte {
	hash := make([]byte, 16, 16)
	n, err := fmt.Sscanf(s, "%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
		&hash[0], &hash[1], &hash[2], &hash[3],
		&hash[4], &hash[5], &hash[6], &hash[7],
		&hash[8], &hash[9], &hash[10], &hash[11],
		&hash[12], &hash[13], &hash[14], &hash[15])
	if n != 16 || err != nil {
		hash = nil
	}
	return hash
}

// Create() returns a pointer to an FsCaBlobStore stored in the file system at
// "rootName".  If the directory rootName does not exist, it is created.
func Create(ctx *context.T, rootName string) (fscabs *FsCaBlobStore, err error) {
	dir := []string{tmpDir, casDir, blobDir}
	for i := 0; i != len(dir) && err == nil; i++ {
		fullName := filepath.Join(rootName, dir[i])
		os.MkdirAll(fullName, dirPermissions)
		var fi os.FileInfo
		fi, err = os.Stat(fullName)
		if err == nil && !fi.IsDir() {
			err = verror.New(errNotADir, ctx, fullName)
		}
	}
	if err == nil {
		fscabs = new(FsCaBlobStore)
		fscabs.rootName = rootName
	}
	return fscabs, err
}

// Root() returns the name of the root directory where *fscabs is stored.
func (fscabs *FsCaBlobStore) Root() string {
	return fscabs.rootName
}

// DeleteBlob() deletes the named blob from *fscabs.
func (fscabs *FsCaBlobStore) DeleteBlob(ctx *context.T, blobName string) (err error) {
	// Disallow deletions of things outside the blob tree, or that may contain "..".
	// For simplicity, the code currently disallows '.'.
	if !strings.HasPrefix(blobName, blobDir+"/") || strings.IndexByte(blobName, '.') != -1 {
		err = verror.New(errInvalidBlobName, ctx, blobName)
	} else {
		err = os.Remove(filepath.Join(fscabs.rootName, blobName))
		if err != nil {
			err = verror.New(errCantDeleteBlob, ctx, blobName, err)
		}
	}
	return err
}

// -----------------------------------------------------------

// A file encapsulates both an os.File and a bufio.Writer on that file.
type file struct {
	fh     *os.File
	writer *bufio.Writer
}

// newFile() returns a *file containing fh and a bufio.Writer on that file, if
// err is nil.
func newFile(fh *os.File, err error) (*file, error) {
	var f *file
	if err == nil {
		f = new(file)
		f.fh = fh
		f.writer = bufio.NewWriter(f.fh)
	}
	return f, err
}

// newTempFile() returns a *file on a new temporary file created in the
// directory dir.
func newTempFile(ctx *context.T, dir string) (*file, error) {
	return newFile(ioutil.TempFile(dir, "newfile"))
}

// close() flushes buffers (if err==nil initially) and closes the file,
// returning its name.
func (f *file) close(ctx *context.T, err error) (string, error) {
	name := f.fh.Name()
	// Flush the data out to disc and close the file.
	if err == nil {
		err = f.writer.Flush()
	}
	if err == nil {
		err = f.fh.Sync()
	}
	err2 := f.fh.Close()
	if err == nil {
		err = err2
	}
	return name, err
}

// closeAndRename() calls f.close(), and if err==nil initially and no new
// errors are seen, renames the file to newName.
func (f *file) closeAndRename(ctx *context.T, newName string, err error) error {
	var oldName string
	oldName, err = f.close(ctx, err)
	if err == nil { // if temp file written successfully...
		// Link or rename the file into place, hoping at least one is
		// supported on this file system.
		os.MkdirAll(filepath.Dir(newName), dirPermissions)
		err = os.Link(oldName, newName)
		if err == nil {
			os.Remove(oldName)
		} else {
			err = os.Rename(oldName, newName)
		}
	}
	if err != nil {
		os.Remove(oldName)
	}
	return err
}

// -----------------------------------------------------------

// A BlockOrFile represents a vector of bytes, and contains either a data block
// (as a []byte), or a (file name, size, offset) triple.
type BlockOrFile struct {
	Block    []byte // If FileName is empty, the bytes represented.
	FileName string // If non-empty, the name of the file containing the bytes.
	Size     int64  // If FileName is non-empty, the number of bytes (or -1 for "all")
	Offset   int64  // If FileName is non-empty, the offset of the relevant bytes within the file.
}

// addFragment() ensures that the store *fscabs contains a fragment comprising
// the catenation of the byte vectors named by item[..].block and the contents
// of the files named by item[..].filename.  The block field is ignored if
// fileName!="".  The fragment is not physically added if already present.
// The fragment is added to the fragment list of the descriptor *desc.
func (fscabs *FsCaBlobStore) addFragment(ctx *context.T, extHasher hash.Hash,
	desc *blobDesc, item ...BlockOrFile) (fileName string, size int64, err error) {

	hasher := md5.New()
	var buf []byte
	var fileHandleList []*os.File

	// Hash the inputs.
	for i := 0; i != len(item) && err == nil; i++ {
		if len(item[i].FileName) != 0 {
			if buf == nil {
				buf = make([]byte, 8192, 8192)
				fileHandleList = make([]*os.File, 0, len(item))
			}
			var fileHandle *os.File
			fileHandle, err = os.Open(filepath.Join(fscabs.rootName, item[i].FileName))
			if err == nil {
				fileHandleList = append(fileHandleList, fileHandle)
				at := item[i].Offset
				toRead := item[i].Size
				var haveRead int64
				for err == nil && (toRead == -1 || haveRead < toRead) {
					var n int
					n, err = fileHandle.ReadAt(buf, at)
					if err == nil {
						if toRead != -1 && int64(n)+haveRead > toRead {
							n = int(toRead - haveRead)
						}
						haveRead += int64(n)
						at += int64(n)
						size += int64(n)
						hasher.Write(buf[0:n]) // Cannot fail; see Hash interface.
						extHasher.Write(buf[0:n])
					}
				}
				if err == io.EOF {
					if toRead == -1 || haveRead == toRead {
						err = nil // The loop read all that was asked; EOF is a possible outcome.
					} else { // The loop read less than was asked; request must have been too big.
						err = verror.New(errSizeTooBigForFragment, ctx, desc.name, item[i].FileName)
					}
				}
			}
		} else {
			hasher.Write(item[i].Block) // Cannot fail; see Hash interface.
			extHasher.Write(item[i].Block)
			size += int64(len(item[i].Block))
		}
	}

	// Compute the hash, and form the file name in the respository.
	hash := hasher.Sum(nil)
	relFileName := hashToFileName(hash)
	absFileName := filepath.Join(fscabs.rootName, relFileName)

	// Add the fragment's name to *desc's fragments so the garbage
	// collector will not delete it.
	fscabs.mu.Lock()
	desc.fragment = append(desc.fragment, blobFragment{
		pos:      desc.size,
		size:     size,
		offset:   0,
		fileName: relFileName})
	desc.size += size
	fscabs.mu.Unlock()

	// If the file does not already exist, ...
	if _, statErr := os.Stat(absFileName); err == nil && os.IsNotExist(statErr) {
		// ... try to create it by writing to a temp file and renaming.
		var t *file
		t, err = newTempFile(ctx, filepath.Join(fscabs.rootName, tmpDir))
		if err == nil {
			// Copy the byte-sequences and input files to the temp file.
			j := 0
			for i := 0; i != len(item) && err == nil; i++ {
				if len(item[i].FileName) != 0 {
					at := item[i].Offset
					toRead := item[i].Size
					var haveRead int64
					for err == nil && (toRead == -1 || haveRead < toRead) {
						var n int
						n, err = fileHandleList[j].ReadAt(buf, at)
						if err == nil {
							if toRead != -1 && int64(n)+haveRead > toRead {
								n = int(toRead - haveRead)
							}
							haveRead += int64(n)
							at += int64(n)
							_, err = t.writer.Write(buf[0:n])
						}
					}
					if err == io.EOF { // EOF is the expected outcome.
						err = nil
					}
					j++
				} else {
					_, err = t.writer.Write(item[i].Block)
				}
			}
			err = t.closeAndRename(ctx, absFileName, err)
		}
	} // else file already exists, nothing more to do.

	for i := 0; i != len(fileHandleList); i++ {
		fileHandleList[i].Close()
	}

	if err != nil {
		err = verror.New(errAppendFailed, ctx, fscabs.rootName, err)
		// Remove the entry added to fragment list above.
		fscabs.mu.Lock()
		desc.fragment = desc.fragment[0 : len(desc.fragment)-1]
		desc.size -= size
		fscabs.mu.Unlock()
	}

	return relFileName, size, err
}

// A blobFragment represents a vector of bytes and its position within a blob.
type blobFragment struct {
	pos      int64  // position of this fragment within its containing blob.
	size     int64  // size of this fragment.
	offset   int64  // offset within fileName.
	fileName string // name of file describing this fragment.
}

// A blobDesc is the in-memory representation of a blob.
type blobDesc struct {
	activeDescIndex int // Index into fscabs.activeDesc if refCount>0; under fscabs.mu.
	refCount        int // Reference count; under fscabs.mu.

	name     string         // Name of the blob.
	fragment []blobFragment // All the fragements in this blob;
	// modified under fscabs.mu and in BlobWriter
	// owner's thread; may be read by GC under
	// fscabs.mu.
	size      int64 // Total size of the blob.
	finalized bool  // Whether the blob has been finalized.
	// A finalized blob has a valid hash field, and no new bytes may be added
	// to it.  A well-formed hash has 16 bytes.
	hash []byte
}

// isBeingDeleted() returns whether fragment fragName is about to be deleted
// by the garbage collector.   Requires fscabs.mu held.
func (fscabs *FsCaBlobStore) isBeingDeleted(fragName string) (beingDeleted bool) {
	for i := 0; i != len(fscabs.toDelete) && !beingDeleted; i++ {
		_, beingDeleted = (*(fscabs.toDelete[i]))[fragName]
	}
	return beingDeleted
}

// descRef() increments the reference count of *desc and returns whether
// successful.  It may fail if the fragments referenced by the descriptor are
// being deleted by the garbage collector.
func (fscabs *FsCaBlobStore) descRef(desc *blobDesc) bool {
	beingDeleted := false
	fscabs.mu.Lock()
	if desc.refCount == 0 {
		// On the first reference, check whether the fragments are
		// being deleted, and if not, add *desc to the
		// fscabs.activeDesc vector.
		for i := 0; i != len(desc.fragment) && !beingDeleted; i++ {
			beingDeleted = fscabs.isBeingDeleted(desc.fragment[i].fileName)
		}
		if !beingDeleted {
			desc.activeDescIndex = len(fscabs.activeDesc)
			fscabs.activeDesc = append(fscabs.activeDesc, desc)
		}
	}
	if !beingDeleted {
		desc.refCount++
	}
	fscabs.mu.Unlock()
	return !beingDeleted
}

// descUnref() decrements the reference count of *desc if desc!=nil; if that
// removes the last reference, *desc is removed from the fscabs.activeDesc
// vector.
func (fscabs *FsCaBlobStore) descUnref(desc *blobDesc) {
	if desc != nil {
		fscabs.mu.Lock()
		desc.refCount--
		if desc.refCount < 0 {
			panic("negative reference count")
		} else if desc.refCount == 0 {
			// Remove desc from fscabs.activeDesc by moving the
			// last entry in fscabs.activeDesc to desc's slot.
			n := len(fscabs.activeDesc)
			lastDesc := fscabs.activeDesc[n-1]
			lastDesc.activeDescIndex = desc.activeDescIndex
			fscabs.activeDesc[desc.activeDescIndex] = lastDesc
			fscabs.activeDesc = fscabs.activeDesc[0 : n-1]
			desc.activeDescIndex = -1
		}
		fscabs.mu.Unlock()
	}
}

// getBlob() returns the in-memory blob descriptor for the named blob.
func (fscabs *FsCaBlobStore) getBlob(ctx *context.T, blobName string) (desc *blobDesc, err error) {
	if !strings.HasPrefix(blobName, blobDir+"/") || strings.IndexByte(blobName, '.') != -1 {
		err = verror.New(errInvalidBlobName, ctx, blobName)
	} else {
		absBlobName := filepath.Join(fscabs.rootName, blobName)
		var fh *os.File
		fh, err = os.Open(absBlobName)
		if err == nil {
			var line string
			desc = new(blobDesc)
			desc.activeDescIndex = -1
			desc.name = blobName
			scanner := bufio.NewScanner(fh)
			for scanner.Scan() {
				field := strings.Split(scanner.Text(), " ")
				if len(field) == 4 && field[0] == "d" {
					var fragSize int64
					var fragOffset int64
					fragSize, err = strconv.ParseInt(field[1], 0, 64)
					if err == nil {
						fragOffset, err = strconv.ParseInt(field[2], 0, 64)
					}
					if err == nil {
						// No locking needed here because desc
						// is newly allocated and not yet passed to descRef().
						desc.fragment = append(desc.fragment,
							blobFragment{
								fileName: field[3],
								pos:      desc.size,
								size:     fragSize,
								offset:   fragOffset})
					}
					desc.size += fragSize
				} else if len(field) == 2 && field[0] == "f" {
					desc.hash = stringToHash(field[1])
					desc.finalized = true
					if desc.hash == nil {
						err = verror.New(errMalformedBlobHash, ctx, blobName, field[1])
					}
				} else if len(field) > 0 && len(field[0]) == 1 && "a" <= field[0] && field[0] <= "z" {
					// unrecognized line, reserved for extensions: ignore.
				} else {
					err = verror.New(errMalformedField, ctx, line)
				}
			}
			err = scanner.Err()
			fh.Close()
		}
	}
	// Ensure that we return either a properly referenced desc, or nil.
	if err != nil {
		desc = nil
	} else if !fscabs.descRef(desc) {
		err = verror.New(errBlobDeleted, ctx, blobName)
		desc = nil
	}
	return desc, err
}

// -----------------------------------------------------------

// A BlobWriter allows a blob to be written.  If a blob has not yet been
// finalized, it also allows that blob to be extended.  A BlobWriter may be
// created with NewBlobWriter(), and should be closed with Close() or
// CloseWithoutFinalize().
type BlobWriter struct {
	// The BlobWriter exists within a particular FsCaBlobStore and context.T
	fscabs *FsCaBlobStore
	ctx    *context.T

	desc   *blobDesc // Description of the blob being written.
	f      *file     // The file being written.
	hasher hash.Hash // Running hash of blob.
}

// NewBlobWriter() returns a pointer to a newly allocated BlobWriter on a newly
// created blob name, which can be found using the Name() method.  BlobWriters
// should not be used concurrently by multiple threads.  The returned handle
// should be closed with either the Close() or CloseWithoutFinalize() method to
// avoid leaking file handles.
func (fscabs *FsCaBlobStore) NewBlobWriter(ctx *context.T) (bw *BlobWriter, err error) {
	newName := newBlobName()
	var f *file
	fileName := filepath.Join(fscabs.rootName, newName)
	os.MkdirAll(filepath.Dir(fileName), dirPermissions)
	f, err = newFile(os.Create(fileName))
	if err == nil {
		bw = new(BlobWriter)
		bw.fscabs = fscabs
		bw.ctx = ctx
		bw.desc = new(blobDesc)
		bw.desc.activeDescIndex = -1
		bw.desc.name = newName
		bw.f = f
		bw.hasher = md5.New()
		if !fscabs.descRef(bw.desc) {
			// Can't happen; descriptor refers to no fragments.
			panic(verror.New(errBlobDeleted, ctx, bw.desc.name))
		}
	}
	return bw, err
}

// ResumeBlobWriter() returns a pointer to a newly allocated BlobWriter on an
// old, but unfinalized blob name.
func (fscabs *FsCaBlobStore) ResumeBlobWriter(ctx *context.T, blobName string) (bw *BlobWriter, err error) {
	bw = new(BlobWriter)
	bw.fscabs = fscabs
	bw.ctx = ctx
	bw.desc, err = bw.fscabs.getBlob(ctx, blobName)
	if err == nil && bw.desc.finalized {
		err = verror.New(errBlobAlreadyFinalized, ctx, blobName)
		bw = nil
	} else {
		fileName := filepath.Join(fscabs.rootName, bw.desc.name)
		bw.f, err = newFile(os.OpenFile(fileName, os.O_WRONLY|os.O_APPEND, 0666))
		bw.hasher = md5.New()
		// Add the existing fragments to the running hash.
		// The descRef's ref count is incremented here to compensate
		// for the decrement it will receive in br.Close(), below.
		if !fscabs.descRef(bw.desc) {
			// Can't happen; descriptor's ref count was already
			// non-zero.
			panic(verror.New(errBlobDeleted, ctx, fileName))
		}
		br := fscabs.blobReaderFromDesc(ctx, bw.desc)
		buf := make([]byte, 8192, 8192)
		for err == nil {
			var n int
			n, err = br.Read(buf)
			bw.hasher.Write(buf[0:n])
		}
		br.Close()
		if err == io.EOF { // EOF is expected.
			err = nil
		}
	}
	return bw, err
}

// Close() finalizes *bw, and indicates that the client will perform no further
// append operations on *bw.  Any internal open file handles are closed.
func (bw *BlobWriter) Close() (err error) {
	if bw.f == nil {
		err = verror.New(errAlreadyClosed, bw.ctx, bw.desc.name)
	} else if bw.desc.finalized {
		err = verror.New(errBlobAlreadyFinalized, bw.ctx, bw.desc.name)
	} else {
		h := bw.hasher.Sum(nil)
		_, err = fmt.Fprintf(bw.f.writer, "f %s\n", hashToString(h)) // finalize
		_, err = bw.f.close(bw.ctx, err)
		bw.f = nil
		bw.desc.finalized = true
		bw.fscabs.descUnref(bw.desc)
	}
	return err
}

// CloseWithoutFinalize() indicates that the client will perform no further
// append operations on *bw, but does not finalize the blob.  Any internal open
// file handles are closed.  Clients are expected to need this operation
// infrequently.
func (bw *BlobWriter) CloseWithoutFinalize() (err error) {
	if bw.f == nil {
		err = verror.New(errAlreadyClosed, bw.ctx, bw.desc.name)
	} else {
		_, err = bw.f.close(bw.ctx, err)
		bw.f = nil
		bw.fscabs.descUnref(bw.desc)
	}
	return err
}

// AppendFragment() appends a fragment to the blob being written by *bw, where
// the fragment is composed of the byte vectors described by the elements of
// item[].  The fragment is copied into the blob store.
func (bw *BlobWriter) AppendFragment(item ...BlockOrFile) (err error) {
	if bw.f == nil {
		panic("fs_cablobstore.BlobWriter programming error: AppendFragment() after Close()")
	}
	var fragmentName string
	var size int64
	fragmentName, size, err = bw.fscabs.addFragment(bw.ctx, bw.hasher, bw.desc, item...)
	if err == nil {
		_, err = fmt.Fprintf(bw.f.writer, "d %d %d %s\n", size, 0 /*offset*/, fragmentName)
	}
	if err == nil {
		err = bw.f.writer.Flush()
	}
	return err
}

// AppendBlob() adds a (substring of a) pre-existing blob to the blob being
// written by *bw.  The fragments of the pre-existing blob are not physically
// copied; they are referenced by both blobs.
func (bw *BlobWriter) AppendBlob(blobName string, size int64, offset int64) (err error) {
	if bw.f == nil {
		panic("fs_cablobstore.BlobWriter programming error: AppendBlob() after Close()")
	}
	var desc *blobDesc
	desc, err = bw.fscabs.getBlob(bw.ctx, blobName)
	origSize := bw.desc.size
	if err == nil {
		if size == -1 {
			size = desc.size - offset
		}
		if offset < 0 || desc.size < offset+size {
			err = verror.New(errBadSizeOrOffset, bw.ctx, size, offset, blobName, desc.size)
		}
		for i := 0; i != len(desc.fragment) && err == nil && size > 0; i++ {
			if desc.fragment[i].size <= offset {
				offset -= desc.fragment[i].size
			} else {
				consume := desc.fragment[i].size - offset
				if size < consume {
					consume = size
				}
				_, err = fmt.Fprintf(bw.f.writer, "d %d %d %s\n",
					consume, offset+desc.fragment[i].offset, desc.fragment[i].fileName)
				if err == nil {
					// Add fragment so garbage collector can see it.
					// The garbage collector cannot be
					// about to delete the fragment, because
					// getBlob() already checked for that
					// above, and kept a reference.
					bw.fscabs.mu.Lock()
					bw.desc.fragment = append(bw.desc.fragment, blobFragment{
						pos:      bw.desc.size,
						size:     consume,
						offset:   offset + desc.fragment[i].offset,
						fileName: desc.fragment[i].fileName})
					bw.desc.size += consume
					bw.fscabs.mu.Unlock()
				}
				offset = 0
				size -= consume
			}
		}
		bw.fscabs.descUnref(desc)
		// Add the new fragments to the running hash.
		if !bw.fscabs.descRef(bw.desc) {
			// Can't happen; descriptor's ref count was already
			// non-zero.
			panic(verror.New(errBlobDeleted, bw.ctx, blobName))
		}
		br := bw.fscabs.blobReaderFromDesc(bw.ctx, bw.desc)
		if err == nil {
			_, err = br.Seek(origSize, 0)
		}
		buf := make([]byte, 8192, 8192)
		for err == nil {
			var n int
			n, err = br.Read(buf)
			bw.hasher.Write(buf[0:n]) // Cannot fail; see Hash interface.
		}
		br.Close()
		if err == io.EOF { // EOF is expected.
			err = nil
		}
		if err == nil {
			err = bw.f.writer.Flush()
		}
	}
	return err
}

// IsFinalized() returns whether *bw has been finalized.
func (bw *BlobWriter) IsFinalized() bool {
	return bw.desc.finalized
}

// Size() returns *bw's size.
func (bw *BlobWriter) Size() int64 {
	return bw.desc.size
}

// Name() returns *bw's name.
func (bw *BlobWriter) Name() string {
	return bw.desc.name
}

// Hash() returns *bw's hash, reflecting the bytes written so far.
func (bw *BlobWriter) Hash() []byte {
	return bw.hasher.Sum(nil)
}

// -----------------------------------------------------------

// A BlobReader allows a blob to be read using the standard ReadAt(), Read(),
// and Seek() calls.  A BlobReader can be created with NewBlobReader(), and
// should be closed with the Close() method to avoid leaking file handles.
type BlobReader struct {
	// The BlobReader exists within a particular FsCaBlobStore and context.T.
	fscabs *FsCaBlobStore
	ctx    *context.T

	desc *blobDesc // A description of the blob being read.

	pos int64 // The next position we will read from (used by Read/Seek, not ReadAt).

	// The fields below represent a cached open fragment desc.fragment[fragmentIndex].
	fragmentIndex int      // -1 or  0 <= fragmentIndex < len(desc.fragment).
	fh            *os.File // non-nil iff fragmentIndex != -1.
}

// blobReaderFromDesc() returns a pointer to a newly allocated BlobReader given
// a pre-existing blobDesc.
func (fscabs *FsCaBlobStore) blobReaderFromDesc(ctx *context.T, desc *blobDesc) *BlobReader {
	br := new(BlobReader)
	br.fscabs = fscabs
	br.ctx = ctx
	br.fragmentIndex = -1
	br.desc = desc
	return br
}

// NewBlobReader() returns a pointer to a newly allocated BlobReader on the
// specified blobName.  BlobReaders should not be used concurrently by multiple
// threads.  Returned handles should be closed with Close().
func (fscabs *FsCaBlobStore) NewBlobReader(ctx *context.T, blobName string) (br *BlobReader, err error) {
	var desc *blobDesc
	desc, err = fscabs.getBlob(ctx, blobName)
	if err == nil {
		br = fscabs.blobReaderFromDesc(ctx, desc)
	}
	return br, err
}

// closeInternal() closes any open file handles within *br.
func (br *BlobReader) closeInternal() {
	if br.fh != nil {
		br.fh.Close()
		br.fh = nil
	}
	br.fragmentIndex = -1
}

// Close() indicates that the client will perform no further operations on *br.
// It closes any open file handles within a BlobReader.
func (br *BlobReader) Close() error {
	br.closeInternal()
	br.fscabs.descUnref(br.desc)
	return nil
}

// findFragment() returns the index of the first element of fragment[] that may
// contain "offset", based on the "pos" fields of each element.
// Requires that fragment[] be sorted on the "pos" fields of the elements.
func findFragment(fragment []blobFragment, offset int64) int {
	lo := 0
	hi := len(fragment)
	for lo < hi {
		mid := (lo + hi) >> 1
		if offset < fragment[mid].pos {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	if lo > 0 {
		lo--
	}
	return lo
}

// ReadAt() fills b[] with up to len(b) bytes of data starting at position "at"
// within the blob that *br indicates, and returns the number of bytes read.
func (br *BlobReader) ReadAt(b []byte, at int64) (n int, err error) {
	i := findFragment(br.desc.fragment, at)
	if i < len(br.desc.fragment) && at <= br.desc.size {
		if i != br.fragmentIndex {
			br.closeInternal()
		}
		if br.fragmentIndex == -1 {
			br.fh, err = os.Open(filepath.Join(br.fscabs.rootName, br.desc.fragment[i].fileName))
			if err == nil {
				br.fragmentIndex = i
			} else {
				br.closeInternal()
			}
		}
		var offset int64 = at - br.desc.fragment[i].pos + br.desc.fragment[i].offset
		consume := br.desc.fragment[i].size - (at - br.desc.fragment[i].pos)
		if int64(len(b)) < consume {
			consume = int64(len(b))
		}
		if br.fh != nil {
			n, err = br.fh.ReadAt(b[0:consume], offset)
		} else if err == nil {
			panic("failed to open blob fragment")
		}
		// Return io.EOF if the Read reached the end of the last
		// fragment, but not if it's merely the end of some interior
		// fragment.
		if int64(n)+at >= br.desc.size {
			if err == nil {
				err = io.EOF
			}
		} else if err == io.EOF {
			err = nil
		}
	} else {
		err = verror.New(errIllegalPositionForRead, br.ctx, br.pos, br.desc.size)
	}
	return n, err
}

// Read() fills b[] with up to len(b) bytes of data starting at the current
// seek position of *br within the blob that *br indicates, and then both
// returns the number of bytes read and advances *br's seek position by that
// amount.
func (br *BlobReader) Read(b []byte) (n int, err error) {
	n, err = br.ReadAt(b, br.pos)
	if err == nil {
		br.pos += int64(n)
	}
	return n, err
}

// Seek() sets the seek position of *br to offset if whence==0,
// offset+current_seek_position if whence==1, and offset+end_of_blob if
// whence==2, and then returns the current seek position.
func (br *BlobReader) Seek(offset int64, whence int) (result int64, err error) {
	if whence == 0 {
		result = offset
	} else if whence == 1 {
		result = offset + br.pos
	} else if whence == 2 {
		result = offset + br.desc.size
	} else {
		err = verror.New(errBadSeekWhence, br.ctx, whence)
		result = br.pos
	}
	if result < 0 {
		err = verror.New(errNegativeSeekPosition, br.ctx, offset, whence)
		result = br.pos
	} else if result > br.desc.size {
		err = verror.New(errIllegalPositionForRead, br.ctx, result, br.desc.size)
		result = br.pos
	} else if err == nil {
		br.pos = result
	}
	return result, err
}

// IsFinalized() returns whether *br has been finalized.
func (br *BlobReader) IsFinalized() bool {
	return br.desc.finalized
}

// Size() returns *br's size.
func (br *BlobReader) Size() int64 {
	return br.desc.size
}

// Name() returns *br's name.
func (br *BlobReader) Name() string {
	return br.desc.name
}

// Hash() returns *br's hash.  It may be nil if the blob is not finalized.
func (br *BlobReader) Hash() []byte {
	return br.desc.hash
}

// -----------------------------------------------------------

// A dirListing is a list of names in a directory, plus a position, which
// indexes the last item in nameList that has been processed.
type dirListing struct {
	pos      int      // Current position in nameList; may be -1 at the start of iteration.
	nameList []string // List of directory entries.
}

// An FsCasIter represents an iterator that allows the client to enumerate all
// the blobs or fragments in a FsCaBlobStore.
type FsCasIter struct {
	fscabs *FsCaBlobStore // The parent FsCaBlobStore.
	err    error          // If non-nil, the error that terminated iteration.
	stack  []dirListing   // The stack of dirListings leading to the current entry.
}

// ListBlobIds() returns an iterator that can be used to enumerate the blobs in
// an FsCaBlobStore.  Expected use is:
//    fscabsi := fscabs.ListBlobIds(ctx)
//    for fscabsi.Advance() {
//      // Process fscabsi.Value() here.
//    }
//    if fscabsi.Err() != nil {
//      // The loop terminated early due to an error.
//    }
func (fscabs *FsCaBlobStore) ListBlobIds(ctx *context.T) (fscabsi *FsCasIter) {
	stack := make([]dirListing, 1)
	stack[0] = dirListing{pos: -1, nameList: []string{blobDir}}
	return &FsCasIter{fscabs: fscabs, stack: stack}
}

// ListCAIds() returns an iterator that can be used to enumerate the
// content-addressable fragments in an FsCaBlobStore.
// Expected use is:
//    fscabsi := fscabs.ListCAIds(ctx)
//    for fscabsi.Advance() {
//      // Process fscabsi.Value() here.
//    }
//    if fscabsi.Err() != nil {
//      // The loop terminated early due to an error.
//    }
func (fscabs *FsCaBlobStore) ListCAIds(ctx *context.T) (fscabsi *FsCasIter) {
	stack := make([]dirListing, 1)
	stack[0] = dirListing{pos: -1, nameList: []string{casDir}}
	return &FsCasIter{fscabs: fscabs, stack: stack}
}

// Advance() stages an item so that it may be retrieved via Value.  Returns
// true iff there is an item to retrieve.  Advance must be called before Value
// is called.
func (fscabsi *FsCasIter) Advance() (advanced bool) {
	stack := fscabsi.stack
	err := fscabsi.err

	for err == nil && !advanced && len(stack) != 0 {
		last := len(stack) - 1
		stack[last].pos++
		if stack[last].pos == len(stack[last].nameList) {
			stack = stack[0:last]
			fscabsi.stack = stack
		} else {
			fullName := filepath.Join(fscabsi.fscabs.rootName, fscabsi.Value())
			var fi os.FileInfo
			fi, err = os.Lstat(fullName)
			if err != nil {
				// error: nothing to do
			} else if fi.IsDir() {
				var dirHandle *os.File
				dirHandle, err = os.Open(fullName)
				if err == nil {
					var nameList []string
					nameList, err = dirHandle.Readdirnames(0)
					dirHandle.Close()
					stack = append(stack, dirListing{pos: -1, nameList: nameList})
					fscabsi.stack = stack
					last = len(stack) - 1
				}
			} else {
				advanced = true
			}
		}
	}

	fscabsi.err = err
	return advanced
}

// Value() returns the item that was staged by Advance.  May panic if Advance
// returned false or was not called.  Never blocks.
func (fscabsi *FsCasIter) Value() (name string) {
	stack := fscabsi.stack
	if fscabsi.err == nil && len(stack) != 0 && stack[0].pos >= 0 {
		name = stack[0].nameList[stack[0].pos]
		for i := 1; i != len(stack); i++ {
			name = filepath.Join(name, stack[i].nameList[stack[i].pos])
		}
	}
	return name
}

// Err() returns any error encountered by Advance.  Never blocks.
func (fscabsi *FsCasIter) Err() error {
	return fscabsi.err
}

// -----------------------------------------------------------

// gcTemp() attempts to delete files in dirName older than threshold.
// Errors are ignored.
func gcTemp(dirName string, threshold time.Time) {
	fh, err := os.Open(dirName)
	if err == nil {
		fi, _ := fh.Readdir(0)
		fh.Close()
		for i := 0; i < len(fi); i++ {
			if fi[i].ModTime().Before(threshold) {
				os.Remove(filepath.Join(dirName, fi[i].Name()))
			}
		}
	}
}

// GC() removes old temp files and content-addressed blocks that are no longer
// referenced by any blob.  It may be called concurrently with other calls to
// GC(), and with uses of BlobReaders and BlobWriters.
func (fscabs *FsCaBlobStore) GC(ctx *context.T) (err error) {
	// Remove old temporary files.
	gcTemp(filepath.Join(fscabs.rootName, tmpDir), time.Now().Add(-10*time.Hour))

	// Add a key to caSet for each content-addressed fragment in *fscabs,
	caSet := make(map[string]bool)
	caIter := fscabs.ListCAIds(ctx)
	for caIter.Advance() {
		caSet[caIter.Value()] = true
	}

	// Remove from caSet all fragments referenced by extant blobs.
	blobIter := fscabs.ListBlobIds(ctx)
	for blobIter.Advance() {
		var blobDesc *blobDesc
		if blobDesc, err = fscabs.getBlob(ctx, blobIter.Value()); err == nil {
			for i := range blobDesc.fragment {
				delete(caSet, blobDesc.fragment[i].fileName)
			}
			fscabs.descUnref(blobDesc)
		}
	}

	if err == nil {
		// Remove from caSet all fragments referenced by open BlobReaders and
		// BlobWriters.  Advertise to new readers and writers which blobs are
		// about to be deleted.
		fscabs.mu.Lock()
		for _, desc := range fscabs.activeDesc {
			for i := range desc.fragment {
				delete(caSet, desc.fragment[i].fileName)
			}
		}
		fscabs.toDelete = append(fscabs.toDelete, &caSet)
		fscabs.mu.Unlock()

		// Delete the things that still remain in caSet; they are no longer
		// referenced.
		for caName := range caSet {
			os.Remove(filepath.Join(fscabs.rootName, caName))
		}

		// Stop advertising what's been deleted.
		fscabs.mu.Lock()
		n := len(fscabs.toDelete)
		var i int
		// We require that &caSet still be in the list.
		for i = 0; fscabs.toDelete[i] != &caSet; i++ {
		}
		fscabs.toDelete[i] = fscabs.toDelete[n-1]
		fscabs.toDelete = fscabs.toDelete[0 : n-1]
		fscabs.mu.Unlock()
	}
	return err
}
