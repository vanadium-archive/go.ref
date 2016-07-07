// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import "container/heap"
import "math/rand"
import "sync"
import "time"

import "v.io/v23/context"
import wire "v.io/v23/services/syncbase"
import "v.io/x/lib/vlog"
import "v.io/x/lib/nsync"
import blob "v.io/x/ref/services/syncbase/localblobstore"
import "v.io/x/ref/services/syncbase/server/interfaces"

// This file contains the machinery that runs in syncgroup "servers" that try
// to fetch blobs within the syncgroup so that they may be stored on the server
// for reliability and availability.

// ---------------------------------

// A BlobFetcherFunc can be passed into the public calls of this abstraction
// to customize how to fetch a blob.
type BlobFetcherFunc func(ctx *context.T, blobRef wire.BlobRef, clientData interface{}) error

// ---------------------------------

// A blobFetchState records the state of a blob that this server is attempting to fetch.
type blobFetchState struct {
	bf  *blobFetcher   // The associated blobFetcher.
	bst blob.BlobStore // The associated blob store.

	blobRef    wire.BlobRef    // the blob's Id.
	clientData interface{}     // provided by client, and passed to fetchFunc
	fetchFunc  BlobFetcherFunc // function to be used to fetch blob

	// The fields below are protected by bf.mu.
	fetchAttempts uint32    // incremented on start of fetch attempt
	stopFetching  bool      // Whether to abandon in-progress fetches, and not restart. Monotonic: false to true.
	nextAttempt   time.Time // time of next attempted fetch.
	err           error     // error recorded on the last fetch.
	heapIndex     int       // index, if in a heap; -1 if being fetched.
}

// ---------------------------------

// A blobFetchStateHeap is a heap of blobFetchState pointers ordered by nextAttempt, earliest first.
type blobFetchStateHeap []*blobFetchState

// The following routines conform to "container/heap".Interface
func (h blobFetchStateHeap) Len() int               { return len(h) }
func (h blobFetchStateHeap) Less(i int, j int) bool { return h[i].nextAttempt.Before(h[j].nextAttempt) }
func (h blobFetchStateHeap) Swap(i int, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}
func (h *blobFetchStateHeap) Push(x interface{}) {
	bfs := x.(*blobFetchState)
	bfs.heapIndex = len(*h)
	*h = append(*h, bfs)
}
func (h *blobFetchStateHeap) Pop() interface{} {
	old := *h
	n := len(old)
	bfs := old[n-1]
	bfs.heapIndex = -1
	*h = old[0 : n-1]
	return bfs
}

// ---------------------------------

// A blobFetcher records the state of all blobs that this server is attempting to fetch.
type blobFetcher struct {
	ctx *context.T // Context passed to NewBlobFetcher().

	mu nsync.Mu // protects fields below, plus most fields in blobFetchState.

	blobMap   map[wire.BlobRef]*blobFetchState // blobs that should be fetched
	blobQueue blobFetchStateHeap               // same elements as blobMap; heap prioritized by nextAttempt time

	maxFetcherThreads int // number of threads allowed to fetch blobs.
	curFetcherThreads int // number of fetcher threads that exist (<= maxFetcherThreads)

	fetchStarterThreadShutdown bool // whether the fetchStarterThread thread has shut down. Monotonic: false to true.

	startFetchThreadCV nsync.CV // a fetch thread can be started (canStartAFetchThread())
	shutdownCV         nsync.CV // shutdown is complete (isShutdown() returns true)
}

// canStartAFetchThread() returns whether a new fetcher thread should be
// started by *bf's fetchStarterThread.  The condition is that the number of
// outstanding fetcher threads is below the maximum, and the next blob to fetch
// has reached its "nextAttempt" deadline.
// Places in *firstBlobDeadline the nextAttempt time of the blob to fetch,
// or nsync.NoDeadline if none.
// Called with bf.mu held.
func (bf *blobFetcher) canStartAFetchThread(firstBlobDeadline *time.Time) (result bool) {
	*firstBlobDeadline = nsync.NoDeadline
	if bf.curFetcherThreads < bf.maxFetcherThreads && len(bf.blobQueue) != 0 { // a chance we could start a thread
		result = bf.blobQueue[0].nextAttempt.Before(time.Now())
		if !result { // failed only because it's not yet time; set the deadline to when it will be time
			*firstBlobDeadline = bf.blobQueue[0].nextAttempt
		}
	}
	return result
}

// isShutdown() returns whether *bf has been shut down.  That is, the fetchStarterThread
// and all fetcher threads have finished.
func (bf *blobFetcher) isShutdown() bool {
	return bf.fetchStarterThreadShutdown && bf.curFetcherThreads == 0
}

// fetchStarterThread() creates threads to fetch blobs (using fetchABlob()).
// It runs until the ctx passed to NewBlobFetcher() becomes cancelled or
// expires.  On exit, it sets bf.fetchStarterThreadShutdown.
func (bf *blobFetcher) fetchStarterThread() {
	bf.mu.Lock()
	for bf.ctx.Err() == nil {
		var deadline time.Time
		// Wait until either we're shut down (bf.ctx.Done() is closed),
		// or we can start a new fetch thread on some blob.
		// canStartAFetchThread() computes the deadline for the CV wait.
		for !bf.canStartAFetchThread(&deadline) &&
			bf.startFetchThreadCV.WaitWithDeadline(&bf.mu, deadline, bf.ctx.Done()) == nsync.OK {
		}
		if bf.ctx.Err() == nil && bf.canStartAFetchThread(&deadline) {
			// Remove the first blob from the priority queue, and start a thread to fetch it.
			var toFetch *blobFetchState = heap.Pop(&bf.blobQueue).(*blobFetchState)
			if toFetch.heapIndex != -1 {
				panic("blobFetchState unexpectedly in heap")
			}
			bf.curFetcherThreads++
			go bf.fetchABlob(toFetch)
		}
	}
	bf.fetchStarterThreadShutdown = true
	if bf.isShutdown() {
		bf.shutdownCV.Broadcast()
	}
	bf.mu.Unlock()
}

// fetchABlob() attempts to fetch the blob identified by *toFetch.  If the
// fetch was unsuccessful and if the client has not requested that fetching be
// abandoned, the fetch is queued for retrying.   Otherwise, the *toFetch
// is removed from *bf.
func (bf *blobFetcher) fetchABlob(toFetch *blobFetchState) {
	bf.mu.Lock()
	if toFetch.heapIndex != -1 {
		panic("blobFetchState unexpectedly on heap")
	}
	bf.mu.Unlock()
	var err error = toFetch.fetchFunc(bf.ctx, toFetch.blobRef, toFetch.clientData)
	bf.mu.Lock()
	toFetch.err = err
	toFetch.fetchAttempts++
	// Maintain fetchAttempts in the on-disc Signpost data structure.
	var sp interfaces.Signpost
	toFetch.bst.GetSignpost(bf.ctx, toFetch.blobRef, &sp)
	// We may write the Signpost back, even if the GetSignpost() call failed.
	// On failure, sp will be a canonical empty Signpost.
	if toFetch.fetchAttempts > sp.FetchAttempts {
		sp.FetchAttempts = toFetch.fetchAttempts
		toFetch.bst.SetSignpost(bf.ctx, toFetch.blobRef, &sp)
	}
	if err == nil || bf.ctx.Err() != nil || toFetch.stopFetching { // fetched blob, or we're told not to retry.
		delete(bf.blobMap, toFetch.blobRef)
	} else { // failed to fetch blob; try again
		toFetch.nextAttempt = time.Now().Add(bf.fetchDelay(toFetch.fetchAttempts))
		heap.Push(&bf.blobQueue, toFetch)
	}
	if toFetch.heapIndex == 0 || bf.curFetcherThreads == bf.maxFetcherThreads {
		// new lowest fetch time, or there were no free threads.
		bf.startFetchThreadCV.Broadcast()
	}
	bf.curFetcherThreads--
	if bf.isShutdown() {
		bf.shutdownCV.Broadcast()
	}
	bf.mu.Unlock()
}

// fetchDelay() returns how long the blobFetcher should wait before attempting to fetch a blob
// for which there have already been "fetchAttempts" failed fetch attempts.
func (bf *blobFetcher) fetchDelay(fetchAttempts uint32) time.Duration {
	// Delay has a random component, and is exponential in failures,
	// between about 1.5s and about 11 days.  (fetchAttempts will be 1 on
	// the first call for a blob, since this is not invoked until there's
	// been a failure.)
	if fetchAttempts > 20 { // Limit delay to around a million seconds---11 days.
		fetchAttempts = 20
	}
	return ((500 + time.Duration(rand.Int31n(500))) * time.Millisecond) << fetchAttempts
}

// StartFetchingBlob() adds a blobRef to blobFetcher *bf, if it's not already
// known to it, and not shutting down.  The client-provided function
// fetchFunc() will be used to fetch the blob.
func (bf *blobFetcher) StartFetchingBlob(bst blob.BlobStore, blobRef wire.BlobRef,
	clientData interface{}, fetchFunc BlobFetcherFunc) {

	bf.mu.Lock()
	var bfs *blobFetchState
	var found bool
	bfs, found = bf.blobMap[blobRef]
	if bf.ctx.Err() == nil && !found {
		bfs = &blobFetchState{
			bf:         bf,
			bst:        bst,
			blobRef:    blobRef,
			clientData: clientData,
			fetchFunc:  fetchFunc,
			heapIndex:  -1,
		}
		var sp interfaces.Signpost
		if err := bst.GetSignpost(bf.ctx, blobRef, &sp); err == nil {
			bfs.fetchAttempts = sp.FetchAttempts
			bfs.nextAttempt = time.Now().Add(bf.fetchDelay(bfs.fetchAttempts))
		}
		bf.blobMap[blobRef] = bfs
		heap.Push(&bf.blobQueue, bfs)
		if bfs.heapIndex == 0 { // a new lowest fetch time
			bf.startFetchThreadCV.Broadcast()
		}
	}
	bf.mu.Unlock()
}

// StopFetchingBlob() removes blobRef from blobFetcher *bf.
// It may still be being fetched, but failures will no longer be retried,
// and an in progress fetch may be halted if possible.
func (bf *blobFetcher) StopFetchingBlob(blobRef wire.BlobRef) {
	bf.mu.Lock()
	var bfs *blobFetchState
	var found bool
	if bfs, found = bf.blobMap[blobRef]; found {
		bfs.stopFetching = true  // tell any in-progress fetcher thread to stop if it can.
		if bfs.heapIndex != -1 { // if not currently fetching, forget blob.
			delete(bf.blobMap, bfs.blobRef)
			heap.Remove(&bf.blobQueue, bfs.heapIndex)
		} // else fetching thread will forget the blob when it finishes.
	}
	bf.mu.Unlock()
}

// NewBlobFetcher() returns a new blobFetcher that can use maxThreads fetcher threads.
func NewBlobFetcher(ctx *context.T, maxThreads int) *blobFetcher {
	bf := &blobFetcher{ctx: ctx, maxFetcherThreads: maxThreads, blobMap: make(map[wire.BlobRef]*blobFetchState)}
	heap.Init(&bf.blobQueue) // "container/heap"'s spec requires this---apparently even on an empty heap.
	go bf.fetchStarterThread()
	return bf
}

// WaitForExit() waits *bf is fully shut down.  Typically, this is used after
// cancelling the context passed to NewBlobFetcher().
func (bf *blobFetcher) WaitForExit() {
	bf.mu.Lock()
	for !bf.isShutdown() {
		bf.shutdownCV.Wait(&bf.mu)
	}
	bf.mu.Unlock()
}

// ---------------------------------

// serverBlobScan() scans the blobs in *s, and for those blobs for which it is
// a server in some syncgroup that fetches the blobs, it gives the blob ids to
// blob fetcher *bf.  Servers are assumed to have space to keep all blobs.
// The function fetchFunc() is used to fetch blobs.
func (s *syncService) serverBlobScan(ctx *context.T, bf *blobFetcher,
	clientData interface{}, fetchFunc BlobFetcherFunc) (err error) {
	// Construct a map that indicates which blobs are available locally,
	// and expunge blobFetchState records for such blobs.
	haveBlob := make(map[wire.BlobRef]bool)
	var bs blob.Stream = s.bst.ListBlobIds(ctx)
	for bs.Advance() && ctx.Err() == nil {
		br, brErr := s.bst.NewBlobReader(ctx, bs.Value())
		if brErr == nil && br.IsFinalized() {
			haveBlob[wire.BlobRef(bs.Value())] = true
		}
	}
	err = bs.Err()
	bs.Cancel() // in case we didn't finish advancing.

	// For every blob whose id has been seen locally, there is a Signpost.
	// Iterate over them, and fetch the ones not available locally for
	// which the current syncbase is a "server" in some syncgroup.
	if err == nil && ctx.Err() == nil {
		var sps blob.SignpostStream = s.bst.NewSignpostStream(ctx)
		for sps.Advance() && ctx.Err() == nil {
			blobRef := sps.BlobId()
			if !haveBlob[blobRef] &&
				len(s.syncgroupsWithServer(ctx, wire.Id{}, s.name, sps.Signpost().SgIds)) != 0 {

				// We do not have the blob locally, and this
				// syncbase is a server in some syncgroup in
				// which the blob has been seen.
				bf.StartFetchingBlob(s.bst, blobRef, clientData, fetchFunc)
			}
		}
		err = sps.Err()
		sps.Cancel() // in case we didn't finish advancing.
	}
	return err
}

// defaultBlobFetcherFunc() is a BlobFetcherFunc that fetches blob blobRef in the normal way.
func defaultBlobFetcherFunc(ctx *context.T, blobRef wire.BlobRef, clientData interface{}) error {
	s := clientData.(*syncService)
	return s.fetchBlobRemote(ctx, blobRef, nil, nil, 0)
}

// ServerBlobFetcher() calls serverBlobScan() repeatedly on ssm (which must contain a *syncService),
// with gaps specified by parameters in parameters.go before scanning them all again.  It
// stops only if the context *ctx is cancelled.  The function fetchFunc() will
// be used to fetch blobs, and passed the argument clientData on each call.
// If done!=nil, done.Done() is called just before the routine returns.
func ServerBlobFetcher(ctx *context.T, ssm interfaces.SyncServerMethods, done *sync.WaitGroup) {
	bf := NewBlobFetcher(ctx, serverBlobFetchConcurrency)
	ss := ssm.(*syncService)
	var delay time.Duration = serverBlobFetchInitialScanDelay
	errCount := 0 // state for limiting log records
	for ctx.Err() == nil {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
		startTime := time.Now()
		if ctx.Err() == nil {
			if err := ss.serverBlobScan(ctx, bf, ss, defaultBlobFetcherFunc); err != nil {
				if (errCount & (errCount - 1)) == 0 { // errCount is 0 or a power of 2.
					vlog.Errorf("ServerBlobFetcher:%d: %v", errCount, err)
				}
				errCount++
			}
		}
		delay = serverBlobFetchExtraScanDelay + serverBlobFetchScanDelayMultiplier*time.Since(startTime)
	}
	bf.WaitForExit()
	if done != nil {
		done.Done()
	}
}
