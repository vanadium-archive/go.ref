// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"bytes"
	"reflect"
	"testing"

	wire "v.io/v23/services/syncbase/nosql"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
	"v.io/x/ref/services/syncbase/store"
)

/*
Test setup:

Group1:
Oid: x, isConflict = true, has Local update, has remote update, has ancestor, resolution = pickLocal

Group2:
Oid: b, isConflict = true, has Local update, remote deleted, has ancestor, resolution = pickRemote

Group3:
Oid: c, isConflict = true, Local deleted, has remote update, has ancestor, resolution = pickRemote

Group4:
Oid: p, isConflict = true, has Local update, has remote update, has ancestor, resolution = createNew

Group5:
Oid: y, isConflict = true, has Local update, has remote update, has no ancestor, resolution = pickRemote
Oid: z, isConflict = false, no local update, has remote update, has unknown ancestor, resolution = pickRemote
Oid: e, isConflict = false, no local update, has remote update, has unknown ancestor, resolution = createNew
Oid: f, isConflict = false, no local update, has remote update, has unknown ancestor, resolution = pickLocal
Oid: g, isConflict = false, no local update, has remote update, has unknown ancestor, local head is deleted, resolution = pickLocal
Oid: a, local value rubberbanded in due to localBatch, resolution = createNew

localBatch: {y, a}
remoteBatch: {y, z, e, f}
*/

var (
	updObjectsAppResolves = map[string]*objConflictState{
		// group1
		x: createObjConflictState(true /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, true /*hasAncestor*/),
		// group2
		b: createObjConflictState(true /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, true /*hasAncestor*/),
		// group3
		c: createObjConflictState(true /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, true /*hasAncestor*/),
		// group4
		p: createObjConflictState(true /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, true /*hasAncestor*/),

		// group5
		y: createObjConflictState(true /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, false /*hasAncestor*/),
		z: createObjConflictState(false /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, false /*hasAncestor*/),
		a: createObjConflictState(false /*isConflict*/, true /*hasLocal*/, false /*hasRemote*/, false /*hasAncestor*/),

		e: createObjConflictState(false /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, false /*hasAncestor*/),
		f: createObjConflictState(false /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, false /*hasAncestor*/),
		g: createObjConflictState(false /*isConflict*/, true /*hasLocal*/, true /*hasRemote*/, false /*hasAncestor*/),
	}

	localBatchId  uint64 = 34
	remoteBatchId uint64 = 58
)

func createGroupedCrTestData() *groupedCrData {
	groupedCrTestData := &groupedCrData{oids: map[string]bool{}}
	var group *crGroup
	// group1
	group = newGroup()
	addToGroup(group, x, NoBatchId, -1)
	groupedCrTestData.oids[x] = true
	groupedCrTestData.groups = append(groupedCrTestData.groups, group)

	// group2
	group = newGroup()
	addToGroup(group, b, NoBatchId, -1)
	groupedCrTestData.oids[b] = true
	groupedCrTestData.groups = append(groupedCrTestData.groups, group)

	// group3
	group = newGroup()
	addToGroup(group, c, NoBatchId, -1)
	groupedCrTestData.oids[c] = true
	groupedCrTestData.groups = append(groupedCrTestData.groups, group)

	// group4
	group = newGroup()
	addToGroup(group, p, NoBatchId, -1)
	groupedCrTestData.oids[p] = true
	groupedCrTestData.groups = append(groupedCrTestData.groups, group)

	// group5
	group = newGroup()
	addToGroup(group, y, localBatchId, wire.BatchSourceLocal)
	addToGroup(group, y, remoteBatchId, wire.BatchSourceRemote)
	addToGroup(group, z, remoteBatchId, wire.BatchSourceRemote)
	addToGroup(group, e, remoteBatchId, wire.BatchSourceRemote)
	addToGroup(group, f, remoteBatchId, wire.BatchSourceRemote)
	addToGroup(group, g, remoteBatchId, wire.BatchSourceRemote)
	addToGroup(group, a, localBatchId, wire.BatchSourceLocal)
	groupedCrTestData.oids[y] = true
	groupedCrTestData.oids[z] = true
	groupedCrTestData.oids[a] = true
	groupedCrTestData.oids[e] = true
	groupedCrTestData.oids[f] = true
	groupedCrTestData.groups = append(groupedCrTestData.groups, group)

	return groupedCrTestData
}

func createAndSaveNodeAndLogRecDataAppResolves(iSt *initiationState) {
	// group1
	saveNodeAndLogRec(iSt.tx, x, updObjectsAppResolves[x].oldHead, 24, false)
	saveNodeAndLogRec(iSt.tx, x, updObjectsAppResolves[x].newHead, 25, false)
	saveNodeAndLogRec(iSt.tx, x, updObjectsAppResolves[x].ancestor, 20, false)

	// group2
	saveNodeAndLogRec(iSt.tx, b, updObjectsAppResolves[b].oldHead, 56, false)
	saveNodeAndLogRec(iSt.tx, b, updObjectsAppResolves[b].newHead, 23, true)
	saveNodeAndLogRec(iSt.tx, b, updObjectsAppResolves[b].ancestor, 15, false)

	// group3
	saveNodeAndLogRec(iSt.tx, c, updObjectsAppResolves[c].oldHead, 56, true)
	saveNodeAndLogRec(iSt.tx, c, updObjectsAppResolves[c].newHead, 23, false)
	saveNodeAndLogRec(iSt.tx, c, updObjectsAppResolves[c].ancestor, 15, false)

	// group4
	saveNodeAndLogRec(iSt.tx, p, updObjectsAppResolves[p].oldHead, 56, false)
	saveNodeAndLogRec(iSt.tx, p, updObjectsAppResolves[p].newHead, 23, false)
	saveNodeAndLogRec(iSt.tx, p, updObjectsAppResolves[p].ancestor, 15, false)

	//group5
	saveNodeAndLogRec(iSt.tx, y, updObjectsAppResolves[y].oldHead, 56, false)
	saveNodeAndLogRec(iSt.tx, y, updObjectsAppResolves[y].newHead, 23, false)

	saveNodeAndLogRec(iSt.tx, z, updObjectsAppResolves[z].oldHead, 56, false)
	saveNodeAndLogRec(iSt.tx, z, updObjectsAppResolves[z].newHead, 23, false)

	saveNodeAndLogRec(iSt.tx, e, updObjectsAppResolves[e].oldHead, 56, false)
	saveNodeAndLogRec(iSt.tx, e, updObjectsAppResolves[e].newHead, 23, false)

	saveNodeAndLogRec(iSt.tx, f, updObjectsAppResolves[f].oldHead, 56, false)
	saveNodeAndLogRec(iSt.tx, f, updObjectsAppResolves[f].newHead, 23, false)

	saveNodeAndLogRec(iSt.tx, g, updObjectsAppResolves[g].oldHead, 56, true)
	saveNodeAndLogRec(iSt.tx, g, updObjectsAppResolves[g].newHead, 23, false)

	saveNodeAndLogRec(iSt.tx, a, updObjectsAppResolves[a].oldHead, 56, false)
}

func writeVersionedValues(t *testing.T, iSt *initiationState) {
	// group1
	saveValue(t, iSt.tx, x, updObjectsAppResolves[x].oldHead)
	saveValue(t, iSt.tx, x, updObjectsAppResolves[x].newHead)
	saveValue(t, iSt.tx, x, updObjectsAppResolves[x].ancestor)

	// group2
	saveValue(t, iSt.tx, b, updObjectsAppResolves[b].oldHead)
	saveValue(t, iSt.tx, b, updObjectsAppResolves[b].ancestor)

	// group3
	saveValue(t, iSt.tx, c, updObjectsAppResolves[c].newHead)
	saveValue(t, iSt.tx, c, updObjectsAppResolves[c].ancestor)

	// group4
	saveValue(t, iSt.tx, p, updObjectsAppResolves[p].oldHead)
	saveValue(t, iSt.tx, p, updObjectsAppResolves[p].newHead)
	saveValue(t, iSt.tx, p, updObjectsAppResolves[p].ancestor)

	// group5
	saveValue(t, iSt.tx, y, updObjectsAppResolves[y].oldHead)
	saveValue(t, iSt.tx, y, updObjectsAppResolves[y].newHead)

	saveValue(t, iSt.tx, z, updObjectsAppResolves[z].oldHead)
	saveValue(t, iSt.tx, z, updObjectsAppResolves[z].newHead)

	saveValue(t, iSt.tx, e, updObjectsAppResolves[e].oldHead)
	saveValue(t, iSt.tx, e, updObjectsAppResolves[e].newHead)

	saveValue(t, iSt.tx, f, updObjectsAppResolves[f].oldHead)
	saveValue(t, iSt.tx, f, updObjectsAppResolves[f].newHead)

	// No value for oldHead for g as g was deleted on local.
	saveValue(t, iSt.tx, g, updObjectsAppResolves[g].newHead)

	saveValue(t, iSt.tx, a, updObjectsAppResolves[a].oldHead)
}

func setResInfoData(mockCrs *conflictResolverStream) {
	// group1
	addResInfo(mockCrs, x, wire.ValueSelectionLocal, nil, false)
	// group2
	addResInfo(mockCrs, b, wire.ValueSelectionRemote, nil, false)
	// group3
	addResInfo(mockCrs, c, wire.ValueSelectionRemote, nil, false)
	// group4
	addResInfo(mockCrs, p, wire.ValueSelectionOther, []byte("newValue"), false)
	// group5
	addResInfo(mockCrs, y, wire.ValueSelectionRemote, nil, true)
	addResInfo(mockCrs, z, wire.ValueSelectionRemote, nil, true)
	addResInfo(mockCrs, e, wire.ValueSelectionOther, []byte("newValue"), true)
	addResInfo(mockCrs, f, wire.ValueSelectionLocal, nil, true)
	addResInfo(mockCrs, g, wire.ValueSelectionLocal, nil, true)
	addResInfo(mockCrs, a, wire.ValueSelectionOther, []byte("newValue"), false)
	setMockCRStream(mockCrs)
}

func TestResolveViaApp(t *testing.T) {
	service := createService(t)
	defer destroyService(t, service)

	iSt := &initiationState{
		updObjects: updObjectsAppResolves,
		tx:         service.St().NewTransaction(),
		config: &initiationConfig{
			sync: service.sync,
			db:   newDb(t, service.sync),
		},
	}
	createAndSaveNodeAndLogRecDataAppResolves(iSt)
	writeVersionedValues(t, iSt)
	mockCrs := &conflictResolverStream{}
	setResInfoData(mockCrs)
	groupedCrTestData := createGroupedCrTestData()

	if err := iSt.resolveViaApp(nil, groupedCrTestData); err != nil {
		t.Errorf("Error returned by resolveViaApp: %v", err)
	}

	// verify
	verifyConflictInfos(t, mockCrs)
	// group1
	verifyResolution(t, iSt.updObjects, x, pickLocal)
	// group2
	verifyResolution(t, iSt.updObjects, b, pickRemote)
	// group3
	verifyResolution(t, iSt.updObjects, c, pickRemote)
	// group4
	verifyCreateNew(t, iSt.updObjects, p, false)
	// group5
	verifyResolution(t, iSt.updObjects, y, pickRemote)
	verifyResolution(t, iSt.updObjects, z, pickRemote)
	verifyCreateNew(t, iSt.updObjects, e, false)
	verifyCreateNew(t, iSt.updObjects, f, false)
	verifyCreateNew(t, iSt.updObjects, g, true)
	verifyCreateNew(t, iSt.updObjects, a, false)

	// verify batch ids
	verifyBatchId(t, iSt.updObjects, NoBatchId, x, b, c, p)
	bid := iSt.updObjects[y].res.batchId
	if bid == NoBatchId {
		t.Errorf("BatchId for group5 should not be NoBatchId")
	}
	verifyBatchId(t, iSt.updObjects, bid, y, z, e, f, a)
}

func verifyConflictInfos(t *testing.T, mockCrs *conflictResolverStream) {
	var ci wire.ConflictInfo
	if len(mockCrs.sendQ) != 12 {
		t.Errorf("ConflictInfo count expected: %v, actual: %v", 9, len(mockCrs.sendQ))
	}

	// group1
	ci = mockCrs.sendQ[0]
	checkConflictRow(t, x, ci, []uint64{}, false /*localDeleted*/, false /*remoteDeleted*/)
	checkContinued(t, mockCrs.sendQ[0:1])

	// group2
	ci = mockCrs.sendQ[1]
	checkConflictRow(t, b, ci, []uint64{}, false /*localDeleted*/, true /*remoteDeleted*/)
	checkContinued(t, mockCrs.sendQ[1:2])

	// group3
	ci = mockCrs.sendQ[2]
	checkConflictRow(t, c, ci, []uint64{}, true /*localDeleted*/, false /*remoteDeleted*/)
	checkContinued(t, mockCrs.sendQ[2:3])

	// group4
	ci = mockCrs.sendQ[3]
	checkConflictRow(t, p, ci, []uint64{}, false /*localDeleted*/, false /*remoteDeleted*/)
	checkContinued(t, mockCrs.sendQ[3:4])

	// group5
	batchMap := map[uint64]wire.ConflictInfo{}
	batchMap[getBid(mockCrs.sendQ[4])] = mockCrs.sendQ[4]
	batchMap[getBid(mockCrs.sendQ[5])] = mockCrs.sendQ[5]
	ci = batchMap[localBatchId]
	checkConflictBatch(t, localBatchId, ci, wire.BatchSourceLocal)
	ci = batchMap[remoteBatchId]
	checkConflictBatch(t, remoteBatchId, ci, wire.BatchSourceRemote)

	rowMap := map[string]wire.ConflictInfo{}
	rowMap[getOid(mockCrs.sendQ[6])] = mockCrs.sendQ[6]
	rowMap[getOid(mockCrs.sendQ[7])] = mockCrs.sendQ[7]
	rowMap[getOid(mockCrs.sendQ[8])] = mockCrs.sendQ[8]
	rowMap[getOid(mockCrs.sendQ[9])] = mockCrs.sendQ[9]
	rowMap[getOid(mockCrs.sendQ[10])] = mockCrs.sendQ[10]
	rowMap[getOid(mockCrs.sendQ[11])] = mockCrs.sendQ[11]
	ci = rowMap[y]
	checkConflictRow(t, y, ci, []uint64{localBatchId, remoteBatchId}, false /*localDeleted*/, false /*remoteDeleted*/)
	ci = rowMap[z]
	checkConflictRow(t, z, ci, []uint64{remoteBatchId}, false /*localDeleted*/, false /*remoteDeleted*/)
	ci = rowMap[a]
	checkConflictRow(t, a, ci, []uint64{localBatchId}, false /*localDeleted*/, false /*remoteDeleted*/)
	ci = rowMap[e]
	checkConflictRow(t, e, ci, []uint64{remoteBatchId}, false /*localDeleted*/, false /*remoteDeleted*/)
	ci = rowMap[f]
	checkConflictRow(t, f, ci, []uint64{remoteBatchId}, false /*localDeleted*/, false /*remoteDeleted*/)
	ci = rowMap[g]
	checkConflictRow(t, g, ci, []uint64{remoteBatchId}, true /*localDeleted*/, false /*remoteDeleted*/)

	checkContinued(t, mockCrs.sendQ[4:])
}

func checkContinued(t *testing.T, infoGroup []wire.ConflictInfo) {
	lastIndex := len(infoGroup) - 1
	for i := range infoGroup {
		if infoGroup[i].Continued != (i != lastIndex) {
			t.Errorf("Wrong value for continued field in %#v", infoGroup[i])
		}
	}
}

func getOid(ci wire.ConflictInfo) string {
	ciData := ci.Data.(wire.ConflictDataRow).Value
	writeOp := ciData.Op.(wire.OperationWrite).Value
	return toRowKey(writeOp.Key)
}

func getBid(ci wire.ConflictInfo) uint64 {
	ciData := ci.Data.(wire.ConflictDataBatch).Value
	return ciData.Id
}

func checkConflictBatch(t *testing.T, bid uint64, ci wire.ConflictInfo, source wire.BatchSource) {
	ciData := ci.Data.(wire.ConflictDataBatch).Value
	if ciData.Source != source {
		t.Errorf("Source for batchid %v expected: %#v, actual: %#v", bid, source, ciData.Source)
	}
	if ci.Continued != true {
		t.Errorf("Bid: %v, Unexpected value for continued: %v", bid, ci.Continued)
	}
}

func checkConflictRow(t *testing.T, oid string, ci wire.ConflictInfo, batchIds []uint64, localDeleted, remoteDeleted bool) {
	ciData := ci.Data.(wire.ConflictDataRow).Value
	writeOp := ciData.Op.(wire.OperationWrite).Value
	st, _ := updObjectsAppResolves[oid]
	if (st.ancestor != NoVersion) && !bytes.Equal(makeValue(oid, st.ancestor), writeOp.AncestorValue.Bytes) {
		t.Errorf("Oid: %v, Ancestor value expected: %v, actual: %v", oid, string(makeValue(oid, st.ancestor)), string(writeOp.AncestorValue.Bytes))
	}
	if remoteDeleted && writeOp.RemoteValue == nil {
		t.Errorf("Oid: %v, for remote deleted remote value is expected to have an instance with no bytes", oid)
	}
	if !remoteDeleted && (st.newHead != NoVersion) && !bytes.Equal(makeValue(oid, st.newHead), writeOp.RemoteValue.Bytes) {
		t.Errorf("Oid: %v, Remote value expected: %v, actual: %v", oid, string(makeValue(oid, st.newHead)), string(writeOp.RemoteValue.Bytes))
	}
	if localDeleted && writeOp.LocalValue == nil {
		t.Errorf("Oid: %v, for local deleted local value is expected to have an instance with no bytes", oid)
	}
	if !localDeleted && (st.oldHead != NoVersion) && !bytes.Equal(makeValue(oid, st.oldHead), writeOp.LocalValue.Bytes) {
		t.Errorf("Oid: %v, Local value expected: %v, actual: %v", oid, string(makeValue(oid, st.oldHead)), string(writeOp.LocalValue.Bytes))
	}
	if !reflect.DeepEqual(ciData.BatchIds, batchIds) {
		t.Errorf("Oid: %v, BatchIds expected: %v, actual: %v", oid, batchIds, ciData.BatchIds)
	}
}

func addResInfo(crs *conflictResolverStream, oid string, sel wire.ValueSelection, val []byte, cntd bool) {
	var valRes *wire.Value
	if val != nil {
		valRes = &wire.Value{
			Bytes: val,
		}
	}
	rInfo := wire.ResolutionInfo{
		Key:       util.StripFirstKeyPartOrDie(oid),
		Selection: sel,
		Result:    valRes,
		Continued: cntd,
	}
	crs.recvQ = append(crs.recvQ, rInfo)
}

func saveValue(t *testing.T, tx store.Transaction, oid, version string) {
	if err := watchable.PutAtVersion(nil, tx, []byte(oid), makeValue(oid, version), []byte(version)); err != nil {
		t.Errorf("Failed to write versioned value for oid,ver: %s,%s", oid, version)
		t.FailNow()
	}
}

func makeValue(oid, ver string) []byte {
	return []byte(oid + ver)
}

func addToGroup(group *crGroup, oid string, bid uint64, source wire.BatchSource) {
	if bid != NoBatchId {
		group.batchSource[bid] = source
		group.batchesByOid[oid] = append(group.batchesByOid[oid], bid)
	} else {
		group.batchesByOid[oid] = []uint64{}
	}
}

func newGroup() *crGroup {
	return &crGroup{
		batchSource:  map[uint64]wire.BatchSource{},
		batchesByOid: map[string][]uint64{},
	}
}

func newDb(t *testing.T, s *syncService) interfaces.Database {
	app, err := s.sv.App(nil, nil, "mockApp")
	if err != nil {
		t.Errorf("Error while creating App: %v", err)
	}
	db, err := app.NoSQLDatabase(nil, nil, "mockDB")
	if err != nil {
		t.Errorf("Error while creating Database: %v", err)
	}
	return db
}
