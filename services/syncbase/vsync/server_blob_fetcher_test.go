// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import "container/heap"
import "math/rand"
import "testing"
import "time"

import "v.io/v23/context"
import wire "v.io/v23/services/syncbase"
import "v.io/v23/verror"
import "v.io/x/lib/nsync"
import blob "v.io/x/ref/services/syncbase/localblobstore"

// TestBlobFetchStateHeap() tests that blobFetchStateHeap() does indeed act as a heap,
// ordered by blobFetchState.nextAttempt
func TestBlobFetchStateHeap(t *testing.T) {
	var queue blobFetchStateHeap
	var list []*blobFetchState
	now := time.Now()
	heap.Init(&queue) // "container/heap"'s spec requires this---apparently even on an empty heap.
	for loop := 0; loop != 1000000; loop++ {
		op := rand.Int31n(4)
		switch op {
		case 0, 1: // add an entry
			bfs := &blobFetchState{nextAttempt: now.Add(time.Duration(rand.Int63()))}
			heap.Push(&queue, bfs)
			i := len(list)
			list = append(list, bfs)
			for i != 0 && bfs.nextAttempt.Before(list[i-1].nextAttempt) {
				list[i] = list[i-1]
				i--
			}
			list[i] = bfs
		case 2: // remove an entry from the root, if there is one
			if len(queue) != 0 {
				bfs := heap.Pop(&queue).(*blobFetchState)
				if bfs != list[0] {
					t.Fatalf("loop %d: Pop: lowest in heap is not lowest in list", loop)
				}
				copy(list[:], list[1:])
				list = list[0 : len(list)-1]
			}
		case 3: // remove an arbitrary entry, if there is one.
			if len(queue) != 0 {
				i := rand.Int31n(int32(len(list)))
				heapIndex := list[i].heapIndex
				bfs := heap.Remove(&queue, heapIndex).(*blobFetchState)
				if bfs != list[i] {
					t.Fatalf("loop %d: Remove() blobFetchState index %d differs from list index %d",
						loop, heapIndex, i)
				}
				copy(list[i:], list[i+1:])
				list = list[0 : len(list)-1]
			}
		}

		// check heap consistency
		if len(queue) != len(list) {
			t.Fatalf("loop %d: len(queue)==%d != %d==len(list)", loop)
		}
		if len(queue) != 0 && queue[0] != list[0] {
			t.Fatalf("loop %d: lowest in heap is not lowest in list", loop)
		}
		for i := 0; i != len(queue); i++ {
			if i != queue[i].heapIndex {
				t.Fatalf("loop %d: heapIndex incorrect: %d != %d", loop, i, queue[i].heapIndex)
			}
		}
	}
}

// A fakeFetchData is passed to fakeBlobFetchFunc() via StartFetchingBlob()'s
// clientData argument.  It holds the data used to fake a fetch, and to
// notify of its completion.
type fakeFetchData struct {
	t     *testing.T
	delay time.Duration // how long each fake fetch waits

	mu                 nsync.Mu             // protects fields below
	fetchesRemaining   int                  // number of outstanding fetches remaining; under mu
	noFetchesRemaining nsync.CV             // signalled when fetchesRemaining==0
	errorsOn           map[wire.BlobRef]int // report an error fetching blob b errorsOn[b] times
}

const pkgPath = "v.io/x/ref/services/syncbase/vsync"

var errFakeBlobFetchError = verror.Register(pkgPath+".fakeBlobFetchError", verror.NoRetry, "{1:}{2:} fakeBlobFetch error")

// fakeBlobFetchFunc() is a fake blob fetching routine.  It's used in this unit test
// to verify that the basic queueing of the server blob fetching mechanism works.
// The real blob fetching path is tested by TestV23ServerBlobFetch in v.io/v23/syncbase/featuretests.
func fakeBlobFetchFunc(ctx *context.T, blobRef wire.BlobRef, ffdi interface{}) error {
	ffd := ffdi.(*fakeFetchData)
	var err error

	time.Sleep(ffd.delay) // simulate a delay in fetching the blob.

	ffd.mu.Lock()
	var errorCount int = ffd.errorsOn[blobRef]
	if errorCount != 0 {
		// If we were instructed to fake an error in fetching this blob, do it.
		ffd.errorsOn[blobRef] = errorCount - 1
		err = verror.New(errFakeBlobFetchError, ctx, blobRef)
	} else {
		// Otherwise, claim success, and report that the fetch of this blob is complete.
		ffd.fetchesRemaining--
		if ffd.fetchesRemaining < 0 {
			panic("finished more fetches than were started")
		}
		if ffd.fetchesRemaining == 0 {
			ffd.noFetchesRemaining.Broadcast()
		}
	}
	ffd.mu.Unlock()

	return err
}

// TestBlobFetch() is a unit test for the blob fetcher, using fake code to actually fetch blobs.
func TestBlobFetchSimple(t *testing.T) {
	svc := createService(t)

	ctx, bfShutdown := context.RootContext()
	defer bfShutdown()

	var ss *syncService = svc.sync
	var bst blob.BlobStore = ss.bst

	const threadCount = 10 // number of threads in BlobFetcher
	bf := NewBlobFetcher(ctx, threadCount)
	ffd := fakeFetchData{
		t:        t,
		delay:    500 * time.Millisecond, // a short delay so test doesn't run too long.
		errorsOn: make(map[wire.BlobRef]int),
	}

	start := time.Now()

	// Create fetchCount BlobRefs and fake a fetch on each.
	// In this initial run, no errors are generated.
	const fetchCount = 100
	var blobRefsFetched []wire.BlobRef
	for i := 0; i != fetchCount; i++ {
		var writer blob.BlobWriter
		var err error
		writer, err = bst.NewBlobWriter(ctx, "")
		if err != nil {
			t.Fatalf("can't make a blob writer: %v\n", err)
		}
		blobRef := wire.BlobRef(writer.Name())
		blobRefsFetched = append(blobRefsFetched, blobRef)
		writer.CloseWithoutFinalize()

		ffd.mu.Lock()
		ffd.fetchesRemaining++
		ffd.mu.Unlock()
		bf.StartFetchingBlob(svc.sync.bst, blobRef, &ffd, fakeBlobFetchFunc)
	}

	// Wait until all fetching is done.  The test would deadlock here if
	// our fetch function was not called the right number of times.
	ffd.mu.Lock()
	for ffd.fetchesRemaining != 0 {
		ffd.noFetchesRemaining.Wait(&ffd.mu)
	}
	ffd.mu.Unlock()
	end := time.Now()

	// Check that the fetching took about the correct amount of time,
	// given the fetch delays and the amount of concurrency requested.
	var elapsed time.Duration = end.Sub(start)
	var expectedElapsed time.Duration = (ffd.delay * fetchCount) / threadCount
	var expectedMinElapsed time.Duration = expectedElapsed / 2
	var expectedMaxElapsed time.Duration = expectedElapsed * 2
	if elapsed < expectedMinElapsed {
		t.Errorf("BlobFetcher completed in %v, expected at least %v", elapsed, expectedMinElapsed)
	}
	if elapsed > expectedMaxElapsed {
		t.Errorf("BlobFetcher completed in %v, expected at most %v", elapsed, expectedMaxElapsed)
	}

	// Now run the test again, but introduce errors on the first fetch of
	// each blob, and issue duplicate requests to fetch each blob.
	start = time.Now()
	ffd.mu.Lock()
	for i := 0; i != fetchCount; i++ {
		ffd.fetchesRemaining++
		ffd.errorsOn[blobRefsFetched[i]] = 1
		for j := 0; j != 3; j++ {
			// Issue duplicate requests; the duplicates will be ignored.
			bf.StartFetchingBlob(svc.sync.bst, blobRefsFetched[i], &ffd, fakeBlobFetchFunc)
		}
	}
	// Wait for fetches to complete.  We would deadlock here if our fetch
	// function didn't ultimately return true for each blob.  The fake test
	// function would panic() if it was called too many times, due to the
	// duplicate requests.
	for ffd.fetchesRemaining != 0 {
		ffd.noFetchesRemaining.Wait(&ffd.mu)
	}
	ffd.mu.Unlock()

	// Check that the fetching took about the correct amount of time,
	// given the fetch delays and the amount of concurrency requested.
	end = time.Now()
	elapsed = end.Sub(start)
	const retryDelay = 1000 * time.Millisecond // min time for first retry, with current parameters.
	expectedElapsed = ((2 * ffd.delay * fetchCount) / threadCount) + retryDelay
	expectedMinElapsed = expectedElapsed / 2
	expectedMaxElapsed = expectedElapsed * 2
	if elapsed < expectedMinElapsed {
		t.Errorf("BlobFetcher completed in %v, expected at least %v", elapsed, expectedMinElapsed)
	}
	if elapsed > expectedMaxElapsed {
		t.Errorf("BlobFetcher completed in %v, expected at most %v", elapsed, expectedMaxElapsed)
	}

	// Shut everything down.
	bfShutdown()
	bf.WaitForExit()
	svc.shutdown()
}
