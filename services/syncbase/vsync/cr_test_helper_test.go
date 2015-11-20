// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"testing"

	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/watchable"
	"v.io/x/ref/services/syncbase/store"
)

var (
	x = makeRowKeyFromParts("table1", "x")
	y = makeRowKeyFromParts("table1", "y")
	z = makeRowKeyFromParts("table1", "z")
	a = makeRowKeyFromParts("table2", "a")
	b = makeRowKeyFromParts("table2", "b")
	c = makeRowKeyFromParts("table2", "c")
	d = makePermsKeyFromParts("table1", "d")

	e = makeRowKeyFromParts("table1", "e")
	f = makeRowKeyFromParts("table1", "f")
	g = makeRowKeyFromParts("table1", "g")

	p = makeRowKeyFromParts("table3", "p")
	q = makeRowKeyFromParts("table3", "q")
)

func createObjConflictState(isConflict, hasLocal, hasRemote, hasAncestor bool) *objConflictState {
	remote := NoVersion
	local := NoVersion
	ancestor := NoVersion
	if hasRemote {
		remote = string(watchable.NewVersion())
	}
	if hasLocal {
		local = string(watchable.NewVersion())
	}
	if hasAncestor {
		ancestor = string(watchable.NewVersion())
	}
	return &objConflictState{
		isAddedByCr: hasLocal && !hasRemote,
		isConflict:  isConflict,
		newHead:     remote,
		oldHead:     local,
		ancestor:    ancestor,
	}
}

func verifyResolution(t *testing.T, updMap map[string]*objConflictState, oid string, resolution resolutionType) {
	st, ok := updMap[oid]
	if !ok {
		t.Errorf("st not found for oid %v", oid)
	}
	if st.res == nil {
		t.Errorf("st.res found nil for oid %v", oid)
		return
	}
	if st.res.ty != resolution {
		t.Errorf("st.res.ty value for oid %v expected: %v, actual: %v", oid, resolution, st.res.ty)
	}
}

func verifyBatchId(t *testing.T, updMap map[string]*objConflictState, batchId uint64, oids ...string) {
	for _, oid := range oids {
		if updMap[oid].res.batchId != batchId {
			t.Errorf("BatchId for Oid %v expected: %#v, actual: %#v", oid, batchId, updMap[oid].res.batchId)
		}
	}
}

func verifyCreateNew(t *testing.T, updMap map[string]*objConflictState, oid string, isDeleted bool) {
	st, ok := updMap[oid]
	if !ok {
		t.Errorf("st not found for oid %v", oid)
	}
	if st.res == nil {
		t.Errorf("st.res found nil for oid %v", oid)
		return
	}
	if st.res.ty != createNew {
		t.Errorf("st.res.ty value for oid %v expected: %v, actual: %v", oid, createNew, st.res.ty)
	}
	if updObjectsAppResolves[oid].res.rec == nil {
		t.Errorf("No log record found for newly created value of oid: %v", oid)
	}
	if isDeleted {
		if updObjectsAppResolves[oid].res.val != nil {
			t.Errorf("Resolution for oid %v has missing new value", oid)
		}
	} else if updObjectsAppResolves[oid].res.val == nil {
		t.Errorf("Resolution for oid %v is not expected to have a val", oid)
	}
}

func saveNodeAndLogRec(tx store.Transaction, oid, version string, ts int64, isDeleted bool) {
	devId, gen := rand64(), rand64()
	node := &DagNode{
		Deleted: isDeleted,
		Logrec:  logRecKey(logDataPrefix, devId, gen),
	}
	setNode(nil, tx, oid, version, node)

	logRec := &LocalLogRec{
		Metadata: interfaces.LogRecMetadata{
			Id:      devId,
			Gen:     gen,
			UpdTime: unixNanoToTime(ts),
			CurVers: version,
		},
	}
	putLogRec(nil, tx, logDataPrefix, logRec)
}
