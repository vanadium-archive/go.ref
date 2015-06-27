// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/store"
)

// Tests for sync state management and storage in Syncbase.

// TestReserveGenAndPos tests reserving generation numbers and log positions in a
// Database log.
func TestReserveGenAndPos(t *testing.T) {
	svc := createService(t)
	defer destroyService(t, svc)
	s := svc.sync

	var wantGen, wantPos uint64 = 1, 0
	for i := 0; i < 5; i++ {
		gotGen, gotPos := s.reserveGenAndPosInternal("mockapp", "mockdb", 5, 10)
		if gotGen != wantGen || gotPos != wantPos {
			t.Fatalf("reserveGenAndPosInternal failed, gotGen %v wantGen %v, gotPos %v wantPos %v", gotGen, wantGen, gotPos, wantPos)
		}
		wantGen += 5
		wantPos += 10

		name := appDbName("mockapp", "mockdb")
		if s.syncState[name].gen != wantGen || s.syncState[name].pos != wantPos {
			t.Fatalf("reserveGenAndPosInternal failed, gotGen %v wantGen %v, gotPos %v wantPos %v", s.syncState[name].gen, wantGen, s.syncState[name].pos, wantPos)
		}
	}
}

// TestPutGetDbSyncState tests setting and getting sync metadata.
func TestPutGetDbSyncState(t *testing.T) {
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()

	checkDbSyncState(t, st, false, nil)

	gv := interfaces.GenVector{
		"mocktbl/foo": interfaces.PrefixGenVector{
			1: 2, 3: 4, 5: 6,
		},
	}

	tx := st.NewTransaction()
	wantSt := &dbSyncState{Gen: 40, GenVec: gv}
	if err := putDbSyncState(nil, tx, wantSt); err != nil {
		t.Fatalf("putDbSyncState failed, err %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("cannot commit putting db sync state, err %v", err)
	}

	checkDbSyncState(t, st, true, wantSt)
}

// TestPutGetDelLogRec tests setting, getting, and deleting a log record.
func TestPutGetDelLogRec(t *testing.T) {
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()

	var id uint64 = 10
	var gen uint64 = 100

	checkLogRec(t, st, id, gen, false, nil)

	tx := st.NewTransaction()
	wantRec := &localLogRec{
		Metadata: interfaces.LogRecMetadata{
			Id:         id,
			Gen:        gen,
			RecType:    interfaces.NodeRec,
			ObjId:      "foo",
			CurVers:    "3",
			Parents:    []string{"1", "2"},
			UpdTime:    time.Now().UTC(),
			Delete:     false,
			BatchId:    10000,
			BatchCount: 1,
		},
		Pos: 10,
	}
	if err := putLogRec(nil, tx, wantRec); err != nil {
		t.Fatalf("putLogRec(%d:%d) failed err %v", id, gen, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("cannot commit putting log rec, err %v", err)
	}

	checkLogRec(t, st, id, gen, true, wantRec)

	tx = st.NewTransaction()
	if err := delLogRec(nil, tx, id, gen); err != nil {
		t.Fatalf("delLogRec(%d:%d) failed err %v", id, gen, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("cannot commit deleting log rec, err %v", err)
	}

	checkLogRec(t, st, id, gen, false, nil)
}

// TestDiffPrefixGenVectors tests diffing prefix gen vectors.
func TestDiffPrefixGenVectors(t *testing.T) {
	svc := createService(t)
	defer destroyService(t, svc)
	s := svc.sync
	s.id = 10 //responder. Initiator is id 11.

	tests := []struct {
		respPVec, initPVec interfaces.PrefixGenVector
		genDiffIn          genRangeVector
		genDiffWant        genRangeVector
	}{
		{ // responder and initiator are at identical vectors.
			respPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			initPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			genDiffIn: make(genRangeVector),
		},
		{ // responder and initiator are at identical vectors.
			respPVec:  interfaces.PrefixGenVector{10: 0},
			initPVec:  interfaces.PrefixGenVector{10: 0},
			genDiffIn: make(genRangeVector),
		},
		{ // responder has no updates.
			respPVec:  interfaces.PrefixGenVector{10: 0},
			initPVec:  interfaces.PrefixGenVector{10: 5, 11: 10, 12: 20, 13: 8},
			genDiffIn: make(genRangeVector),
		},
		{ // responder and initiator have no updates.
			respPVec:  interfaces.PrefixGenVector{10: 0},
			initPVec:  interfaces.PrefixGenVector{11: 0},
			genDiffIn: make(genRangeVector),
		},
		{ // responder is staler than initiator.
			respPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			initPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 8, 14: 5},
			genDiffIn: make(genRangeVector),
		},
		{ // responder is more up-to-date than initiator for local updates.
			respPVec:    interfaces.PrefixGenVector{10: 5, 11: 10, 12: 20, 13: 2},
			initPVec:    interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			genDiffIn:   make(genRangeVector),
			genDiffWant: genRangeVector{10: &genRange{min: 2, max: 5}},
		},
		{ // responder is fresher than initiator for local updates and one device.
			respPVec:  interfaces.PrefixGenVector{10: 5, 11: 10, 12: 22, 13: 2},
			initPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2, 14: 40},
			genDiffIn: make(genRangeVector),
			genDiffWant: genRangeVector{
				10: &genRange{min: 2, max: 5},
				12: &genRange{min: 21, max: 22},
			},
		},
		{ // responder is fresher than initiator in all but one device.
			respPVec:  interfaces.PrefixGenVector{10: 1, 11: 2, 12: 3, 13: 4},
			initPVec:  interfaces.PrefixGenVector{10: 0, 11: 2, 12: 0},
			genDiffIn: make(genRangeVector),
			genDiffWant: genRangeVector{
				10: &genRange{min: 1, max: 1},
				12: &genRange{min: 1, max: 3},
				13: &genRange{min: 1, max: 4},
			},
		},
		{ // initiator has no updates.
			respPVec:  interfaces.PrefixGenVector{10: 1, 11: 2, 12: 3, 13: 4},
			initPVec:  interfaces.PrefixGenVector{},
			genDiffIn: make(genRangeVector),
			genDiffWant: genRangeVector{
				10: &genRange{min: 1, max: 1},
				11: &genRange{min: 1, max: 2},
				12: &genRange{min: 1, max: 3},
				13: &genRange{min: 1, max: 4},
			},
		},
		{ // initiator has no updates, pre-existing diff.
			respPVec: interfaces.PrefixGenVector{10: 1, 11: 2, 12: 3, 13: 4},
			initPVec: interfaces.PrefixGenVector{13: 1},
			genDiffIn: genRangeVector{
				10: &genRange{min: 5, max: 20},
				13: &genRange{min: 1, max: 3},
			},
			genDiffWant: genRangeVector{
				10: &genRange{min: 1, max: 20},
				11: &genRange{min: 1, max: 2},
				12: &genRange{min: 1, max: 3},
				13: &genRange{min: 1, max: 4},
			},
		},
	}

	for _, test := range tests {
		want := test.genDiffWant
		got := test.genDiffIn
		s.diffPrefixGenVectors(test.respPVec, test.initPVec, got)
		checkEqualDevRanges(t, got, want)
	}
}

// TestSendDeltas tests the computation of the delta bound (computeDeltaBound)
// and if the log records on the wire are correctly ordered.
func TestSendDeltas(t *testing.T) {
	appName := "mockapp"
	dbName := "mockdb"

	tests := []struct {
		respVec, initVec, outVec interfaces.GenVector
		respGen                  uint64
		genDiff                  genRangeVector
		keyPfxs                  []string
	}{
		{ // Identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{12: 8},
				"foobar": interfaces.PrefixGenVector{12: 10},
			},
			initVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{11: 5},
				"foobar": interfaces.PrefixGenVector{11: 5},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 8},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 10},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 1, max: 10},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", ""},
		},
		{ // Identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"bar":    interfaces.PrefixGenVector{12: 20},
				"foo":    interfaces.PrefixGenVector{12: 8},
				"foobar": interfaces.PrefixGenVector{12: 10},
			},
			initVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{11: 5},
				"foobar": interfaces.PrefixGenVector{11: 5, 12: 10},
				"bar":    interfaces.PrefixGenVector{10: 5, 11: 5, 12: 5},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 8},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 10},
				"bar":    interfaces.PrefixGenVector{10: 5, 12: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 1, max: 20},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "bar", "barbaz", ""},
		},
		{ // Non-identical prefixes, local only updates.
			initVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{11: 5},
				"foobar": interfaces.PrefixGenVector{11: 5},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{10: 5},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "fooxyz"},
		},
		{ // Non-identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"f":      interfaces.PrefixGenVector{12: 5, 13: 5},
				"foo":    interfaces.PrefixGenVector{12: 10, 13: 10},
				"foobar": interfaces.PrefixGenVector{12: 20, 13: 20},
			},
			initVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{11: 5, 12: 1},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 10, 13: 10},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 20, 13: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 20},
				13: &genRange{min: 1, max: 20},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "fooxyz"},
		},
		{ // Non-identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"foobar": interfaces.PrefixGenVector{12: 20, 13: 20},
			},
			initVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{11: 5, 12: 1},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 20, 13: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 20},
				13: &genRange{min: 1, max: 20},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "fooxyz"},
		},
		{ // Non-identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"f": interfaces.PrefixGenVector{12: 20, 13: 20},
			},
			initVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{11: 5, 12: 1},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{10: 5, 12: 20, 13: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 20},
				13: &genRange{min: 1, max: 20},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "fooxyz"},
		},
		{ // Non-identical interleaving prefixes.
			respVec: interfaces.GenVector{
				"f":      interfaces.PrefixGenVector{12: 20, 13: 10},
				"foo":    interfaces.PrefixGenVector{12: 30, 13: 20},
				"foobar": interfaces.PrefixGenVector{12: 40, 13: 30},
			},
			initVec: interfaces.GenVector{
				"fo":        interfaces.PrefixGenVector{11: 5, 12: 1},
				"foob":      interfaces.PrefixGenVector{11: 5, 12: 10},
				"foobarxyz": interfaces.PrefixGenVector{11: 5, 12: 20},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"fo":     interfaces.PrefixGenVector{10: 5, 12: 20, 13: 10},
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 30, 13: 20},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 40, 13: 30},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 40},
				13: &genRange{min: 1, max: 30},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "foob", "foobarxyz", "fooxyz"},
		},
		{ // Non-identical interleaving prefixes.
			respVec: interfaces.GenVector{
				"fo":        interfaces.PrefixGenVector{12: 20, 13: 10},
				"foob":      interfaces.PrefixGenVector{12: 30, 13: 20},
				"foobarxyz": interfaces.PrefixGenVector{12: 40, 13: 30},
			},
			initVec: interfaces.GenVector{
				"f":      interfaces.PrefixGenVector{11: 5, 12: 1},
				"foo":    interfaces.PrefixGenVector{11: 5, 12: 10},
				"foobar": interfaces.PrefixGenVector{11: 5, 12: 20},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"f":         interfaces.PrefixGenVector{10: 5},
				"fo":        interfaces.PrefixGenVector{10: 5, 12: 20, 13: 10},
				"foob":      interfaces.PrefixGenVector{10: 5, 12: 30, 13: 20},
				"foobarxyz": interfaces.PrefixGenVector{10: 5, 12: 40, 13: 30},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 40},
				13: &genRange{min: 1, max: 30},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "foob", "foobarxyz", "fooxyz"},
		},
		{ // Non-identical sibling prefixes.
			respVec: interfaces.GenVector{
				"foo":       interfaces.PrefixGenVector{12: 20, 13: 10},
				"foobarabc": interfaces.PrefixGenVector{12: 40, 13: 30},
				"foobarxyz": interfaces.PrefixGenVector{12: 30, 13: 20},
			},
			initVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{11: 5, 12: 1},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":       interfaces.PrefixGenVector{10: 5, 12: 20, 13: 10},
				"foobarabc": interfaces.PrefixGenVector{10: 5, 12: 40, 13: 30},
				"foobarxyz": interfaces.PrefixGenVector{10: 5, 12: 30, 13: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 40},
				13: &genRange{min: 1, max: 30},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "foobarabc", "foobarxyz", "foobar123", "fooxyz"},
		},
		{ // Non-identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"barbaz": interfaces.PrefixGenVector{12: 18},
				"f":      interfaces.PrefixGenVector{12: 30, 13: 5},
				"foobar": interfaces.PrefixGenVector{12: 30, 13: 8},
			},
			initVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{11: 5, 12: 5},
				"foobar": interfaces.PrefixGenVector{11: 5, 12: 5},
				"bar":    interfaces.PrefixGenVector{10: 5, 11: 5, 12: 5},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 30, 13: 5},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 30, 13: 8},
				"bar":    interfaces.PrefixGenVector{10: 5},
				"barbaz": interfaces.PrefixGenVector{10: 5, 12: 18},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 6, max: 30},
				13: &genRange{min: 1, max: 8},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "bar", "barbaz", ""},
		},
	}

	for i, test := range tests {
		svc := createService(t)
		s := svc.sync
		s.id = 10 //responder.

		wantDiff, wantVec := test.genDiff, test.outVec
		s.syncState[appDbName(appName, dbName)] = &dbSyncStateInMem{gen: test.respGen, ckPtGen: test.respGen, genvec: test.respVec}

		gotDiff, gotVec, err := s.computeDeltaBound(nil, appName, dbName, test.initVec)
		if err != nil || !reflect.DeepEqual(gotVec, wantVec) {
			t.Fatalf("computeDeltaBound failed (I: %v), (R: %v, %v), got %v, want %v err %v", test.initVec, test.respGen, test.respVec, gotVec, wantVec, err)
		}
		checkEqualDevRanges(t, gotDiff, wantDiff)

		// Insert some log records to bootstrap testing below.
		tRng := rand.New(rand.NewSource(int64(i)))
		var wantRecs []*localLogRec
		st := svc.St()
		tx := st.NewTransaction()
		objKeyPfxs := test.keyPfxs
		j := 0
		for id, r := range wantDiff {
			pos := uint64(tRng.Intn(50) + 100*j)
			for k := r.min; k <= r.max; k++ {
				opfx := objKeyPfxs[tRng.Intn(len(objKeyPfxs))]
				// Create holes in the log records.
				if opfx == "" {
					continue
				}
				okey := fmt.Sprintf("%s~%x", opfx, tRng.Int())
				vers := fmt.Sprintf("%x", tRng.Int())
				rec := &localLogRec{
					Metadata: interfaces.LogRecMetadata{Id: id, Gen: k, ObjId: okey, CurVers: vers, UpdTime: time.Now().UTC()},
					Pos:      pos + k,
				}
				if err := putLogRec(nil, tx, rec); err != nil {
					t.Fatalf("putLogRec(%d:%d) failed rec %v err %v", id, k, rec, err)
				}

				initPfxs := extractAndSortPrefixes(test.initVec)
				if !filterLogRec(rec, test.initVec, initPfxs) {
					wantRecs = append(wantRecs, rec)
				}
			}
			j++
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("cannot commit putting log rec, err %v", err)
		}

		ts := &logRecStreamTest{}
		gotVec, err = s.sendDeltasPerDatabase(nil, nil, appName, dbName, test.initVec, ts)
		if err != nil || !reflect.DeepEqual(gotVec, wantVec) {
			t.Fatalf("sendDeltasPerDatabase failed (I: %v), (R: %v, %v), got %v, want %v err %v", test.initVec, test.respGen, test.respVec, gotVec, wantVec, err)
		}
		ts.diffLogRecs(t, wantRecs)

		destroyService(t, svc)
	}
}

//////////////////////////////
// Helpers

// TODO(hpucha): Look into using v.io/syncbase/v23/syncbase/testutil.Fatalf()
// for getting the stack trace. Right now cannot import the package due to a
// cycle.

type logRecStreamTest struct {
	gotRecs []*localLogRec
}

func (s *logRecStreamTest) Send(rec interfaces.LogRec) {
	s.gotRecs = append(s.gotRecs, &localLogRec{Metadata: rec.Metadata})
}

func (s *logRecStreamTest) diffLogRecs(t *testing.T, wantRecs []*localLogRec) {
	if len(s.gotRecs) != len(wantRecs) {
		t.Fatalf("diffLogRecMetadata failed, gotLen %v, wantLen %v\n", len(s.gotRecs), len(wantRecs))
	}
	for i, rec := range s.gotRecs {
		if !reflect.DeepEqual(rec.Metadata, wantRecs[i].Metadata) {
			t.Fatalf("diffLogRecMetadata failed, i %v, got %v, want %v\n", i, rec.Metadata, wantRecs[i].Metadata)
		}
	}
}

func checkDbSyncState(t *testing.T, st store.StoreReader, exists bool, wantSt *dbSyncState) {
	gotSt, err := getDbSyncState(nil, st)

	if (!exists && err == nil) || (exists && err != nil) {
		t.Fatalf("getDbSyncState failed, exists %v err %v", exists, err)
	}

	if !reflect.DeepEqual(gotSt, wantSt) {
		t.Fatalf("getDbSyncState() failed, got %v, want %v", gotSt, wantSt)
	}
}

func checkLogRec(t *testing.T, st store.StoreReader, id, gen uint64, exists bool, wantRec *localLogRec) {
	gotRec, err := getLogRec(nil, st, id, gen)

	if (!exists && err == nil) || (exists && err != nil) {
		t.Fatalf("getLogRec(%d:%d) failed, exists %v err %v", id, gen, exists, err)
	}

	if !reflect.DeepEqual(gotRec, wantRec) {
		t.Fatalf("getLogRec(%d:%d) failed, got %v, want %v", id, gen, gotRec, wantRec)
	}

	if hasLogRec(st, id, gen) != exists {
		t.Fatalf("hasLogRec(%d:%d) failed, want %v", id, gen, exists)
	}
}

func checkEqualDevRanges(t *testing.T, s1, s2 genRangeVector) {
	if len(s1) != len(s2) {
		t.Fatalf("len(s1): %v != len(s2): %v", len(s1), len(s2))
	}
	for d1, r1 := range s1 {
		if r2, ok := s2[d1]; !ok || !reflect.DeepEqual(r1, r2) {
			t.Fatalf("Dev %v: r1 %v != r2 %v", d1, r1, r2)
		}
	}
}
