// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"container/heap"
	"sort"
	"strings"

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/verror"
)

// GetDeltas implements the responder side of the GetDeltas RPC.
func (s *syncService) GetDeltas(ctx *context.T, call interfaces.SyncGetDeltasServerCall) error {
	recvr := call.RecvStream()

	for recvr.Advance() {
		req := recvr.Value()
		// Ignoring errors since if one Database fails for any reason,
		// it is fine to continue to the next one. In fact, sometimes
		// the failure might be genuine. For example, the responder is
		// no longer part of the requested SyncGroups, or the app/db is
		// locally deleted, or a permission change has denied access.
		rSt := newResponderState(ctx, call, s, req)
		rSt.sendDeltasPerDatabase(ctx)
	}

	// TODO(hpucha): Is there a need to call finish or some such?
	return recvr.Err()
}

// responderState is state accumulated per Database by the responder during an
// initiation round.
type responderState struct {
	req      interfaces.DeltaReq
	call     interfaces.SyncGetDeltasServerCall // Stream handle for the GetDeltas RPC.
	errState error                              // Captures the error from the first two phases of the responder.
	sync     *syncService
	st       store.Store // Store handle to the Database.
	diff     genRangeVector
	outVec   interfaces.GenVector
}

func newResponderState(ctx *context.T, call interfaces.SyncGetDeltasServerCall, sync *syncService, req interfaces.DeltaReq) *responderState {
	rSt := &responderState{call: call, sync: sync, req: req}
	return rSt
}

// sendDeltasPerDatabase sends to an initiator all the missing generations
// corresponding to the prefixes requested for this Database, and a genvector
// summarizing the knowledge transferred from the responder to the
// initiator. This happens in three phases:
//
// In the first phase, the initiator is checked against the SyncGroup ACLs of
// all the SyncGroups it is requesting, and only those prefixes that belong to
// allowed SyncGroups are carried forward.
//
// In the second phase, for a given set of nested prefixes from the initiator,
// the shortest prefix in that set is extracted. The initiator's prefix
// genvector for this shortest prefix represents the lower bound on its
// knowledge for the entire set of nested prefixes. This prefix genvector
// (representing the lower bound) is diffed with all the responder prefix
// genvectors corresponding to same or deeper prefixes compared to the initiator
// prefix. This diff produces a bound on the missing knowledge. For example, say
// the initiator is interested in prefixes {foo, foobar}, where each prefix is
// associated with a prefix genvector. Since the initiator strictly has as much
// or more knowledge for prefix "foobar" as it has for prefix "foo", "foo"'s
// prefix genvector is chosen as the lower bound for the initiator's
// knowledge. Similarly, say the responder has knowledge on prefixes {f,
// foobarX, foobarY, bar}. The responder diffs the prefix genvectors for
// prefixes f, foobarX and foobarY with the initiator's prefix genvector to
// compute a bound on missing generations (all responder's prefixes that match
// "foo". Note that since the responder doesn't have a prefix genvector at
// "foo", its knowledge at "f" is applicable to "foo").
//
// Since the second phase outputs an aggressive calculation of missing
// generations containing more generation entries than strictly needed by the
// initiator, in the third phase, each missing generation is sent to the
// initiator only if the initiator is eligible for it and is not aware of
// it. The generations are sent to the initiator in the same order as the
// responder learned them so that the initiator can reconstruct the DAG for the
// objects by learning older nodes first.
func (rSt *responderState) sendDeltasPerDatabase(ctx *context.T) error {
	// Phase 1 of sendDeltas: Authorize the initiator and respond to the
	// caller only for the SyncGroups that allow access.
	rSt.authorizeAndFilterSyncGroups(ctx)

	// Phase 2 of sendDeltas: diff contains the bound on the
	// generations missing from the initiator per device.
	rSt.computeDeltaBound(ctx)

	// Phase 3 of sendDeltas: Process the diff, filtering out records that
	// are not needed, and send the remainder on the wire ordered.
	return rSt.filterAndSendDeltas(ctx)
}

// authorizeAndFilterSyncGroups authorizes the initiator against the requested
// SyncGroups and filters the initiator's prefixes to only include those from
// allowed SyncGroups (phase 1 of sendDeltas).
func (rSt *responderState) authorizeAndFilterSyncGroups(ctx *context.T) {
	rSt.st, rSt.errState = rSt.sync.getDbStore(ctx, nil, rSt.req.AppName, rSt.req.DbName)
	if rSt.errState != nil {
		return
	}

	allowedPfxs := make(map[string]struct{})
	for sgid := range rSt.req.SgIds {
		// Check permissions for the SyncGroup.
		var sg *interfaces.SyncGroup
		sg, rSt.errState = getSyncGroupById(ctx, rSt.st, sgid)
		if rSt.errState != nil {
			return
		}
		rSt.errState = authorize(ctx, rSt.call.Security(), sg)
		if verror.ErrorID(rSt.errState) == verror.ErrNoAccess.ID {
			continue
		} else if rSt.errState != nil {
			return
		}

		for _, p := range sg.Spec.Prefixes {
			allowedPfxs[p] = struct{}{}
		}
	}

	// Filter the initiator's prefixes to what is allowed.
	for pfx := range rSt.req.InitVec {
		if _, ok := allowedPfxs[pfx]; ok {
			continue
		}
		allowed := false
		for p := range allowedPfxs {
			if strings.HasPrefix(pfx, p) {
				allowed = true
			}
		}

		if !allowed {
			delete(rSt.req.InitVec, pfx)
		}
	}
	return
}

// computeDeltaBound computes the bound on missing generations across all
// requested prefixes (phase 2 of sendDeltas).
func (rSt *responderState) computeDeltaBound(ctx *context.T) {
	// Check error from phase 1.
	if rSt.errState != nil {
		return
	}

	if len(rSt.req.InitVec) == 0 {
		rSt.errState = verror.New(verror.ErrInternal, ctx, "empty initiator generation vector")
		return
	}

	var respVec interfaces.GenVector
	var respGen uint64
	respVec, respGen, rSt.errState = rSt.sync.getDbGenInfo(ctx, rSt.req.AppName, rSt.req.DbName)
	if rSt.errState != nil {
		return
	}
	respPfxs := extractAndSortPrefixes(respVec)
	initPfxs := extractAndSortPrefixes(rSt.req.InitVec)

	rSt.outVec = make(interfaces.GenVector)
	rSt.diff = make(genRangeVector)
	pfx := initPfxs[0]

	for _, p := range initPfxs {
		if strings.HasPrefix(p, pfx) && p != pfx {
			continue
		}

		// Process this prefix as this is the start of a new set of
		// nested prefixes.
		pfx = p

		// Lower bound on initiator's knowledge for this prefix set.
		initpgv := rSt.req.InitVec[pfx]

		// Find the relevant responder prefixes and add the corresponding knowledge.
		var respgv interfaces.PrefixGenVector
		var rpStart string
		for _, rp := range respPfxs {
			if !strings.HasPrefix(rp, pfx) && !strings.HasPrefix(pfx, rp) {
				// No relationship with pfx.
				continue
			}

			if strings.HasPrefix(pfx, rp) {
				// If rp is a prefix of pfx, remember it because
				// it may be a potential starting point for the
				// responder's knowledge. The actual starting
				// point is the deepest prefix where rp is a
				// prefix of pfx.
				//
				// Say the initiator is looking for "foo", and
				// the responder has knowledge for "f" and "fo",
				// the responder's starting point will be the
				// prefix genvector for "fo". Similarly, if the
				// responder has knowledge for "foo", the
				// starting point will be the prefix genvector
				// for "foo".
				rpStart = rp
			} else {
				// If pfx is a prefix of rp, this knowledge must
				// be definitely sent to the initiator. Diff the
				// prefix genvectors to adjust the delta bound and
				// include in outVec.
				respgv = respVec[rp]
				rSt.diffPrefixGenVectors(respgv, initpgv)
				rSt.outVec[rp] = respgv
			}
		}

		// Deal with the starting point.
		if rpStart == "" {
			// No matching prefixes for pfx were found.
			respgv = make(interfaces.PrefixGenVector)
			respgv[rSt.sync.id] = respGen
		} else {
			respgv = respVec[rpStart]
		}
		rSt.diffPrefixGenVectors(respgv, initpgv)
		rSt.outVec[pfx] = respgv
	}

	return
}

// filterAndSendDeltas filters the computed delta to remove records already
// known by the initiator, and sends the resulting records to the initiator
// (phase 3 of sendDeltas).
func (rSt *responderState) filterAndSendDeltas(ctx *context.T) error {
	// Always send a start and finish response so that the initiator can
	// move on to the next Database.
	//
	// TODO(hpucha): Although ok for now to call SendStream once per
	// Database, would like to make this implementation agnostic.
	sender := rSt.call.SendStream()
	sender.Send(interfaces.DeltaRespStart{true})
	defer sender.Send(interfaces.DeltaRespFinish{true})

	// Check error from phase 2.
	if rSt.errState != nil {
		return rSt.errState
	}

	// First two phases were successful. So now on to phase 3. We now visit
	// every log record in the generation range as obtained from phase 1 in
	// their log order. We use a heap to incrementally sort the log records
	// as per their position in the log.
	//
	// Init the min heap, one entry per device in the diff.
	mh := make(minHeap, 0, len(rSt.diff))
	for dev, r := range rSt.diff {
		r.cur = r.min
		rec, err := getNextLogRec(ctx, rSt.st, dev, r)
		if err != nil {
			return err
		}
		if rec != nil {
			mh = append(mh, rec)
		} else {
			delete(rSt.diff, dev)
		}
	}
	heap.Init(&mh)

	// Process the log records in order.
	initPfxs := extractAndSortPrefixes(rSt.req.InitVec)
	for mh.Len() > 0 {
		rec := heap.Pop(&mh).(*localLogRec)

		if !filterLogRec(rec, rSt.req.InitVec, initPfxs) {
			// Send on the wire.
			wireRec := interfaces.LogRec{Metadata: rec.Metadata}
			// TODO(hpucha): Hash out this fake stream stuff when
			// defining the RPC and the rest of the responder.
			sender.Send(interfaces.DeltaRespRec{wireRec})
		}

		// Add a new record from the same device if not done.
		dev := rec.Metadata.Id
		rec, err := getNextLogRec(ctx, rSt.st, dev, rSt.diff[dev])
		if err != nil {
			return err
		}
		if rec != nil {
			heap.Push(&mh, rec)
		} else {
			delete(rSt.diff, dev)
		}
	}

	sender.Send(interfaces.DeltaRespRespVec{rSt.outVec})
	return nil
}

// genRange represents a range of generations (min and max inclusive).
type genRange struct {
	min uint64
	max uint64
	cur uint64
}

type genRangeVector map[uint64]*genRange

// diffPrefixGenVectors diffs two generation vectors, belonging to the responder
// and the initiator, and updates the range of generations per device known to
// the responder but not known to the initiator. "gens" (generation range) is
// passed in as an input argument so that it can be incrementally updated as the
// range of missing generations grows when different responder prefix genvectors
// are used to compute the diff.
//
// For example: Generation vector for responder is say RVec = {A:10, B:5, C:1},
// Generation vector for initiator is say IVec = {A:5, B:10, D:2}. Diffing these
// two vectors returns: {A:[6-10], C:[1-1]}.
//
// TODO(hpucha): Add reclaimVec for GCing.
func (rSt *responderState) diffPrefixGenVectors(respPVec, initPVec interfaces.PrefixGenVector) {
	// Compute missing generations for devices that are in both initiator's and responder's vectors.
	for devid, gen := range initPVec {
		rgen, ok := respPVec[devid]
		if ok {
			updateDevRange(devid, rgen, gen, rSt.diff)
		}
	}

	// Compute missing generations for devices not in initiator's vector but in responder's vector.
	for devid, rgen := range respPVec {
		if _, ok := initPVec[devid]; !ok {
			updateDevRange(devid, rgen, 0, rSt.diff)
		}
	}
}

func updateDevRange(devid, rgen, gen uint64, gens genRangeVector) {
	if gen < rgen {
		// Need to include all generations in the interval [gen+1,rgen], gen+1 and rgen inclusive.
		if r, ok := gens[devid]; !ok {
			gens[devid] = &genRange{min: gen + 1, max: rgen}
		} else {
			if gen+1 < r.min {
				r.min = gen + 1
			}
			if rgen > r.max {
				r.max = rgen
			}
		}
	}
}

func extractAndSortPrefixes(vec interfaces.GenVector) []string {
	pfxs := make([]string, len(vec))
	i := 0
	for p := range vec {
		pfxs[i] = p
		i++
	}
	sort.Strings(pfxs)
	return pfxs
}

// TODO(hpucha): This can be optimized using a scan instead of "gets" in a for
// loop.
func getNextLogRec(ctx *context.T, sn store.StoreReader, dev uint64, r *genRange) (*localLogRec, error) {
	for i := r.cur; i <= r.max; i++ {
		rec, err := getLogRec(ctx, sn, dev, i)
		if err == nil {
			r.cur = i + 1
			return rec, nil
		}
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			return nil, err
		}
	}
	return nil, nil
}

// Note: initPfxs is sorted.
func filterLogRec(rec *localLogRec, initVec interfaces.GenVector, initPfxs []string) bool {
	filter := true

	var maxGen uint64
	for _, p := range initPfxs {
		if strings.HasPrefix(rec.Metadata.ObjId, p) {
			// Do not filter. Initiator is interested in this
			// prefix.
			filter = false

			// Track if the initiator knows of this record.
			gen := initVec[p][rec.Metadata.Id]
			if maxGen < gen {
				maxGen = gen
			}
		}
	}

	// Filter this record if the initiator already has it.
	if maxGen >= rec.Metadata.Gen {
		return true
	}

	return filter
}

// A minHeap implements heap.Interface and holds local log records.
type minHeap []*localLogRec

func (mh minHeap) Len() int { return len(mh) }

func (mh minHeap) Less(i, j int) bool {
	return mh[i].Pos < mh[j].Pos
}

func (mh minHeap) Swap(i, j int) {
	mh[i], mh[j] = mh[j], mh[i]
}

func (mh *minHeap) Push(x interface{}) {
	item := x.(*localLogRec)
	*mh = append(*mh, item)
}

func (mh *minHeap) Pop() interface{} {
	old := *mh
	n := len(old)
	item := old[n-1]
	*mh = old[0 : n-1]
	return item
}
