// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The initiator tests below are driven by replaying the state from the log
// files (in testdata directory). These log files may mimic watching the
// Database locally (addl commands in the log file) or obtaining log records and
// generation vector from a remote peer (addr, genvec commands). The log files
// contain the metadata of log records. The log files are only used to set up
// the state. The tests verify that given a particular local state and a stream
// of remote deltas, the initiator behaves as expected.

package vsync

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	wire "v.io/v23/services/syncbase/nosql"
	"v.io/v23/vdl"
	"v.io/v23/vom"
	"v.io/x/lib/set"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
)

func TestExtractBlobRefs(t *testing.T) {
	var tests [][]byte
	br := wire.BlobRef("123")

	// BlobRef is the value.
	buf0, err := vom.Encode(br)
	if err != nil {
		t.Fatalf("Encode(BlobRef) failed, err %v", err)
	}
	tests = append(tests, buf0)

	// Struct contains BlobRef.
	type test1Struct struct {
		A int64
		B string
		C wire.BlobRef
	}
	v1 := test1Struct{A: 10, B: "foo", C: br}
	buf1, err := vom.Encode(v1)
	if err != nil {
		t.Fatalf("Encode(test1Struct) failed, err %v", err)
	}
	tests = append(tests, buf1)

	// Nested struct contains BlobRef.
	type test2Struct struct {
		A int64
		B string
		C test1Struct
	}
	v2 := test2Struct{A: 10, B: "foo", C: v1}
	buf2, err := vom.Encode(v2)
	if err != nil {
		t.Fatalf("Encode(test2Struct) failed, err %v", err)
	}
	tests = append(tests, buf2)

	for i, buf := range tests {
		var val *vdl.Value
		if err := vom.Decode(buf, &val); err != nil {
			t.Fatalf("Decode failed (test %d), err %v", i, err)
		}

		gotbrs := make(map[wire.BlobRef]struct{})
		if err := extractBlobRefs(val, gotbrs); err != nil {
			t.Fatalf("extractBlobRefs failed (test %d), err %v", i, err)
		}
		wantbrs := map[wire.BlobRef]struct{}{br: struct{}{}}
		if !reflect.DeepEqual(gotbrs, wantbrs) {
			t.Fatalf("Data mismatch in blobrefs (test %d), got %v, want %v", i, gotbrs, wantbrs)
		}
	}
}

// TestLogStreamRemoteOnly tests processing of a remote log stream. Commands are
// in file testdata/remote-init-00.log.sync.
func TestLogStreamRemoteOnly(t *testing.T) {
	svc, iSt, cleanup := testInit(t, "", "remote-init-00.log.sync", false)
	defer cleanup()

	// Check all log records.
	objid := util.JoinKeyParts(util.RowPrefix, "tb", "foo1")
	var gen uint64
	var parents []string
	for gen = 1; gen < 4; gen++ {
		gotRec, err := getLogRec(nil, svc.St(), logDataPrefix, 11, gen)
		if err != nil || gotRec == nil {
			t.Fatalf("getLogRec can not find object %s 11 %d, err %v", logDataPrefix, gen, err)
		}
		vers := fmt.Sprintf("%d", gen)
		wantRec := &localLogRec{
			Metadata: interfaces.LogRecMetadata{
				Id:         11,
				Gen:        gen,
				RecType:    interfaces.NodeRec,
				ObjId:      objid,
				CurVers:    vers,
				Parents:    parents,
				UpdTime:    constTime,
				BatchCount: 1,
			},
			Pos: gen - 1,
		}

		if !reflect.DeepEqual(gotRec, wantRec) {
			t.Fatalf("Data mismatch in log record got %v, want %v", gotRec, wantRec)
		}
		// Verify DAG state.
		if _, err := getNode(nil, svc.St(), objid, vers); err != nil {
			t.Fatalf("getNode can not find object %s vers %s in DAG, err %v", objid, vers, err)
		}
		// Verify Database state.
		tx := svc.St().NewTransaction()
		if _, err := watchable.GetAtVersion(nil, tx, []byte(objid), nil, []byte(vers)); err != nil {
			t.Fatalf("GetAtVersion can not find object %s vers %s in Database, err %v", objid, vers, err)
		}
		tx.Abort()
		parents = []string{vers}
	}

	// Verify conflict state.
	if len(iSt.updObjects) != 1 {
		t.Fatalf("Unexpected number of updated objects %d", len(iSt.updObjects))
	}
	st := iSt.updObjects[objid]
	if st.isConflict {
		t.Fatalf("Detected a conflict %v", st)
	}
	if st.newHead != "3" || st.oldHead != NoVersion {
		t.Fatalf("Conflict detection didn't succeed %v", st)
	}

	// Verify genvec state.
	wantVec := interfaces.GenVector{
		"tb\xfefoo1": interfaces.PrefixGenVector{11: 3},
		"tb\xfebar":  interfaces.PrefixGenVector{11: 0},
	}
	if !reflect.DeepEqual(iSt.updLocal, wantVec) {
		t.Fatalf("Final local gen vec mismatch got %v, want %v", iSt.updLocal, wantVec)
	}

	// Verify DAG state.
	if head, err := getHead(nil, svc.St(), objid); err != nil || head != "3" {
		t.Fatalf("Invalid object %s head in DAG %v, err %v", objid, head, err)
	}

	// Verify Database state.
	valbuf, err := svc.St().Get([]byte(objid), nil)
	var val string
	if err := vom.Decode(valbuf, &val); err != nil {
		t.Fatalf("Value decode failed, err %v", err)
	}
	if err != nil || val != "abc" {
		t.Fatalf("Invalid object %s in Database %v, err %v", objid, val, err)
	}
	tx := svc.St().NewTransaction()
	version, err := watchable.GetVersion(nil, tx, []byte(objid))
	if err != nil || string(version) != "3" {
		t.Fatalf("Invalid object %s head in Database %v, err %v", objid, string(version), err)
	}
	tx.Abort()
}

// TestLogStreamNoConflict tests that a local and a remote log stream can be
// correctly applied (when there are no conflicts). Commands are in files
// testdata/<local-init-00.log.sync,remote-noconf-00.log.sync>.
func TestLogStreamNoConflict(t *testing.T) {
	svc, iSt, cleanup := testInit(t, "local-init-00.log.sync", "remote-noconf-00.log.sync", false)
	defer cleanup()

	objid := util.JoinKeyParts(util.RowPrefix, "tb\xfefoo1")

	// Check all log records.
	var version uint64 = 1
	var parents []string
	for _, devid := range []uint64{10, 11} {
		var gen uint64
		for gen = 1; gen < 4; gen++ {
			gotRec, err := getLogRec(nil, svc.St(), logDataPrefix, devid, gen)
			if err != nil || gotRec == nil {
				t.Fatalf("getLogRec can not find object %s:%d:%d, err %v",
					logDataPrefix, devid, gen, err)
			}
			vers := fmt.Sprintf("%d", version)
			wantRec := &localLogRec{
				Metadata: interfaces.LogRecMetadata{
					Id:         devid,
					Gen:        gen,
					RecType:    interfaces.NodeRec,
					ObjId:      objid,
					CurVers:    vers,
					Parents:    parents,
					UpdTime:    constTime,
					BatchCount: 1,
				},
				Pos: gen - 1,
			}

			if !reflect.DeepEqual(gotRec, wantRec) {
				t.Fatalf("Data mismatch in log record got %v, want %v", gotRec, wantRec)
			}

			// Verify DAG state.
			if _, err := getNode(nil, svc.St(), objid, vers); err != nil {
				t.Fatalf("getNode can not find object %s vers %s in DAG, err %v", objid, vers, err)
			}
			// Verify Database state.
			tx := svc.St().NewTransaction()
			if _, err := watchable.GetAtVersion(nil, tx, []byte(objid), nil, []byte(vers)); err != nil {
				t.Fatalf("GetAtVersion can not find object %s vers %s in Database, err %v", objid, vers, err)
			}
			tx.Abort()
			parents = []string{vers}
			version++
		}
	}

	// Verify conflict state.
	if len(iSt.updObjects) != 1 {
		t.Fatalf("Unexpected number of updated objects %d", len(iSt.updObjects))
	}
	st := iSt.updObjects[objid]
	if st.isConflict {
		t.Fatalf("Detected a conflict %v", st)
	}
	if st.newHead != "6" || st.oldHead != "3" {
		t.Fatalf("Conflict detection didn't succeed %v", st)
	}

	// Verify genvec state.
	wantVec := interfaces.GenVector{
		"tb\xfefoo1": interfaces.PrefixGenVector{11: 3},
		"tb\xfebar":  interfaces.PrefixGenVector{11: 0},
	}
	if !reflect.DeepEqual(iSt.updLocal, wantVec) {
		t.Fatalf("Final local gen vec failed got %v, want %v", iSt.updLocal, wantVec)
	}

	// Verify DAG state.
	if head, err := getHead(nil, svc.St(), objid); err != nil || head != "6" {
		t.Fatalf("Invalid object %s head in DAG %v, err %v", objid, head, err)
	}

	// Verify Database state.
	valbuf, err := svc.St().Get([]byte(objid), nil)
	var val string
	if err := vom.Decode(valbuf, &val); err != nil {
		t.Fatalf("Value decode failed, err %v", err)
	}
	if err != nil || val != "abc" {
		t.Fatalf("Invalid object %s in Database %v, err %v", objid, val, err)
	}
	tx := svc.St().NewTransaction()
	versbuf, err := watchable.GetVersion(nil, tx, []byte(objid))
	if err != nil || string(versbuf) != "6" {
		t.Fatalf("Invalid object %s head in Database %v, err %v", objid, string(versbuf), err)
	}
	tx.Abort()
}

// TestLogStreamConflict tests that a local and a remote log stream can be
// correctly applied when there are conflicts. Commands are in files
// testdata/<local-init-00.log.sync,remote-conf-00.log.sync>.
func TestLogStreamConflict(t *testing.T) {
	svc, iSt, cleanup := testInit(t, "local-init-00.log.sync", "remote-conf-00.log.sync", false)
	defer cleanup()

	objid := util.JoinKeyParts(util.RowPrefix, "tb\xfefoo1")

	// Verify conflict state.
	if len(iSt.updObjects) != 1 {
		t.Fatalf("Unexpected number of updated objects %d", len(iSt.updObjects))
	}
	st := iSt.updObjects[objid]
	if !st.isConflict {
		t.Fatalf("Didn't detect a conflict %v", st)
	}
	if st.newHead != "6" || st.oldHead != "3" || st.ancestor != "2" {
		t.Fatalf("Conflict detection didn't succeed %v", st)
	}
	if st.res.ty != pickRemote {
		t.Fatalf("Conflict resolution did not pick remote: %v", st.res.ty)
	}

	// Verify DAG state.
	if head, err := getHead(nil, svc.St(), objid); err != nil || head != "6" {
		t.Fatalf("Invalid object %s head in DAG %v, err %v", objid, head, err)
	}

	// Verify Database state.
	valbuf, err := svc.St().Get([]byte(objid), nil)
	var val string
	if err := vom.Decode(valbuf, &val); err != nil {
		t.Fatalf("Value decode failed, err %v", err)
	}
	if err != nil || val != "abc" {
		t.Fatalf("Invalid object %s in Database %v, err %v", objid, string(valbuf), err)
	}
	tx := svc.St().NewTransaction()
	versbuf, err := watchable.GetVersion(nil, tx, []byte(objid))
	if err != nil || string(versbuf) != "6" {
		t.Fatalf("Invalid object %s head in Database %v, err %v", objid, string(versbuf), err)
	}
	tx.Abort()
}

// TestLogStreamConflictNoAncestor tests that a local and a remote log stream
// can be correctly applied when there are conflicts from the start where the
// two versions of an object have no common ancestor. Commands are in files
// testdata/<local-init-00.log.sync,remote-conf-03.log.sync>.
func TestLogStreamConflictNoAncestor(t *testing.T) {
	svc, iSt, cleanup := testInit(t, "local-init-00.log.sync", "remote-conf-03.log.sync", false)
	defer cleanup()

	objid := util.JoinKeyParts(util.RowPrefix, "tb\xfefoo1")

	// Verify conflict state.
	if len(iSt.updObjects) != 1 {
		t.Fatalf("Unexpected number of updated objects %d", len(iSt.updObjects))
	}
	st := iSt.updObjects[objid]
	if !st.isConflict {
		t.Fatalf("Didn't detect a conflict %v", st)
	}
	if st.newHead != "6" || st.oldHead != "3" || st.ancestor != "" {
		t.Fatalf("Conflict detection didn't succeed %v", st)
	}
	if st.res.ty != pickRemote {
		t.Fatalf("Conflict resolution did not pick remote: %v", st.res.ty)
	}

	// Verify DAG state.
	if head, err := getHead(nil, svc.St(), objid); err != nil || head != "6" {
		t.Fatalf("Invalid object %s head in DAG %v, err %v", objid, head, err)
	}

	// Verify Database state.
	valbuf, err := svc.St().Get([]byte(objid), nil)
	var val string
	if err := vom.Decode(valbuf, &val); err != nil {
		t.Fatalf("Value decode failed, err %v", err)
	}
	if err != nil || val != "abc" {
		t.Fatalf("Invalid object %s in Database %v, err %v", objid, string(valbuf), err)
	}
	tx := svc.St().NewTransaction()
	versbuf, err := watchable.GetVersion(nil, tx, []byte(objid))
	if err != nil || string(versbuf) != "6" {
		t.Fatalf("Invalid object %s head in Database %v, err %v", objid, string(versbuf), err)
	}
	tx.Abort()
}

//////////////////////////////
// Helpers.

func testInit(t *testing.T, lfile, rfile string, sg bool) (*mockService, *initiationState, func()) {
	// Set a large value to prevent the initiator from running.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	cleanup := func() {
		destroyService(t, svc)
	}

	// If there's an error during testInit, we bail out and should clean up after
	// ourselves.
	var err error
	defer func() {
		if err != nil {
			cleanup()
		}
	}()

	s := svc.sync
	s.id = 10 // initiator

	sgId1 := interfaces.GroupId(1234)
	gdb := appDbName("mockapp", "mockdb")
	nullInfo := wire.SyncgroupMemberInfo{}
	sgInfo := sgMemberInfo{
		sgId1: nullInfo,
	}
	info := &memberInfo{
		db2sg: map[string]sgMemberInfo{
			gdb: sgInfo,
		},
		mtTables: map[string]struct{}{
			"1/2/3/4": struct{}{},
			"5/6/7/8": struct{}{},
		},
	}

	sg1 := &interfaces.Syncgroup{
		Name:        "sg1",
		Id:          sgId1,
		AppName:     "mockapp",
		DbName:      "mockdb",
		Creator:     "mockCreator",
		SpecVersion: "etag-0",
		Spec: wire.SyncgroupSpec{
			Prefixes:    []wire.TableRow{{TableName: "foo", Row: ""}, {TableName: "bar", Row: ""}},
			MountTables: []string{"1/2/3/4", "5/6/7/8"},
		},
		Joiners: map[string]wire.SyncgroupMemberInfo{
			"a": nullInfo,
			"b": nullInfo,
		},
	}

	tx := svc.St().NewTransaction()
	if err = s.addSyncgroup(nil, tx, NoVersion, true, "", nil, s.id, 1, 1, sg1); err != nil {
		t.Fatalf("cannot add syncgroup ID %d, err %v", sg1.Id, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("cannot commit adding syncgroup ID %d, err %v", sg1.Id, err)
	}

	if lfile != "" {
		replayLocalCommands(t, svc, lfile)
	}

	if rfile == "" {
		return svc, nil, cleanup
	}

	var c *initiationConfig
	if c, err = newInitiationConfig(nil, s, gdb, info.db2sg[gdb], connInfo{relName: "b"}, set.String.ToSlice(info.mtTables)); err != nil {
		t.Fatalf("newInitiationConfig failed with err %v", err)
	}

	iSt := newInitiationState(nil, c, sg)

	sgs := make(sgSet)
	sgs[sgId1] = struct{}{}
	iSt.config.sgPfxs = map[string]sgSet{
		toTableRowPrefixStr(sg1.Spec.Prefixes[0]): sgs,
		toTableRowPrefixStr(sg1.Spec.Prefixes[1]): sgs,
	}

	sort.Strings(iSt.config.mtTbls)
	sort.Strings(sg1.Spec.MountTables)

	if !reflect.DeepEqual(iSt.config.mtTbls, sg1.Spec.MountTables) {
		// Set err so that the deferred cleanup func runs.
		err = fmt.Errorf("Mount tables are not equal: config %v, spec %v", iSt.config.mtTbls, sg1.Spec.MountTables)
		t.Fatal(err)
	}

	s.initSyncStateInMem(nil, "mockapp", "mockdb", sgOID(sgId1))

	iSt.stream = createReplayStream(t, rfile)

	var wantVec interfaces.GenVector
	if sg {
		if err = iSt.prepareSGDeltaReq(nil); err != nil {
			t.Fatalf("prepareSGDeltaReq failed with err %v", err)
		}
		sg := fmt.Sprintf("%d", sgId1)
		wantVec = interfaces.GenVector{
			sg: interfaces.PrefixGenVector{10: 0},
		}
	} else {
		if err = iSt.prepareDataDeltaReq(nil); err != nil {
			t.Fatalf("prepareDataDeltaReq failed with err %v", err)
		}

		wantVec = interfaces.GenVector{
			"foo\xfe": interfaces.PrefixGenVector{10: 0},
			"bar\xfe": interfaces.PrefixGenVector{10: 0},
		}
	}

	if !reflect.DeepEqual(iSt.local, wantVec) {
		// Set err so that the deferred cleanup func runs.
		err = fmt.Errorf("createLocalGenVec failed: got %v, want %v", iSt.local, wantVec)
		t.Fatal(err)
	}

	if err = iSt.recvAndProcessDeltas(nil); err != nil {
		t.Fatalf("recvAndProcessDeltas failed with err %v", err)
	}

	if err = iSt.processUpdatedObjects(nil); err != nil {
		t.Fatalf("processUpdatedObjects failed with err %v", err)
	}
	return svc, iSt, cleanup
}

func testIfSgPfxsEqual(t *testing.T, m map[string]sgSet, a []string) {
	aMap := arrToMap(a)

	if len(aMap) != len(m) {
		t.Fatalf("testIfSgPfxsEqual diff lengths: got %v, want %v", aMap, m)
	}

	for p := range aMap {
		if _, ok := m[p]; !ok {
			t.Fatalf("testIfSgPfxsEqual: want %v", p)
		}
	}
}

func arrToMap(a []string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, s := range a {
		m[s] = struct{}{}
	}
	return m
}
