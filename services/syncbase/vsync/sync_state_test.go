// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
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

//////////////////////////////
// Helpers

// TODO(hpucha): Look into using v.io/syncbase/v23/syncbase/testutil.Fatalf()
// for getting the stack trace. Right now cannot import the package due to a
// cycle.

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
