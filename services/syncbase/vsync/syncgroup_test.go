// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for syncgroup management and storage in Syncbase.

import (
	"reflect"
	"strings"
	"testing"
	"time"

	wire "v.io/v23/services/syncbase"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/store/watchable"
)

// checkSGStats verifies syncgroup stats.
func checkSGStats(t *testing.T, svc *mockService, which string, numSG, numMembers int) {
	memberViewTTL = 0 // Always recompute the syncgroup membership view.
	svc.sync.refreshMembersIfExpired(nil)

	view := svc.sync.allMembers
	if num := len(view.members); num != numMembers {
		t.Errorf("num-members (%s): got %v instead of %v", which, num, numMembers)
	}

	sgids := make(map[interfaces.GroupId]bool)
	for _, info := range view.members {
		for _, sgmi := range info.db2sg {
			for gid := range sgmi {
				sgids[gid] = true
			}
		}
	}

	if num := len(sgids); num != numSG {
		t.Errorf("num-syncgroups (%s): got %v instead of %v", which, num, numSG)
	}
}

// TestAddSyncgroup tests adding syncgroups.
func TestAddSyncgroup(t *testing.T) {
	// Set a large value to prevent the syncer from running. Since this
	// test adds a fake syncgroup, if the syncer runs, it will attempt
	// to initiate using this fake and partial syncgroup data.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	defer destroyService(t, svc)
	st := createDatabase(t, svc).St()
	s := svc.sync

	checkSGStats(t, svc, "add-1", 0, 0)

	// Add a syncgroup.

	sgName := "foobar"
	version := "v111"

	sg := &interfaces.Syncgroup{
		Name:        sgName,
		DbId:        mockDbId,
		Creator:     "mockCreator",
		SpecVersion: "etag-0",
		Spec: wire.SyncgroupSpec{
			Prefixes: []wire.CollectionRow{{CollectionId: makeCxId("foo"), Row: ""}, {CollectionId: makeCxId("bar"), Row: ""}},
		},
		Joiners: map[string]wire.SyncgroupMemberInfo{
			"phone":  wire.SyncgroupMemberInfo{SyncPriority: 10},
			"tablet": wire.SyncgroupMemberInfo{SyncPriority: 25},
			"cloud":  wire.SyncgroupMemberInfo{SyncPriority: 1},
		},
	}

	tx := st.NewWatchableTransaction()
	if err := s.addSyncgroup(nil, tx, version, true, "", nil, s.id, 1, 1, sg); err != nil {
		t.Errorf("cannot add syncgroup %s: %v", sg.Name, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding syncgroup %s: %v", sg.Name, err)
	}

	// Verify syncgroup ID, name, and data.
	sgId := sgNameToId(sgName)

	sgOut, err := getSyncgroupById(nil, st, sgId)
	if err != nil {
		t.Errorf("cannot get syncgroup by ID %s: %v", sgId, err)
	}
	if !reflect.DeepEqual(sgOut, sg) {
		t.Errorf("invalid syncgroup data for group ID %s: got %v instead of %v", sgId, sgOut, sg)
	}

	sgOut, err = getSyncgroupByName(nil, st, sgName)
	if err != nil {
		t.Errorf("cannot get syncgroup by Name %s: %v", sgName, err)
	}
	if !reflect.DeepEqual(sgOut, sg) {
		t.Errorf("invalid syncgroup data for group name %s: got %v instead of %v", sgName, sgOut, sg)
	}

	// Verify membership data.
	// Force a rescan of membership data.
	s.allMembers = nil

	expMembers := map[string]uint32{"phone": 1, "tablet": 1, "cloud": 1}

	members := svc.sync.getMembers(nil)
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid syncgroup members: got %v instead of %v", members, expMembers)
	}

	view := svc.sync.allMembers
	for mm := range members {
		mi := view.members[mm]
		if mi == nil {
			t.Errorf("cannot get info for syncgroup member %s", mm)
		}
		if len(mi.db2sg) != 1 {
			t.Errorf("invalid info for syncgroup member %s: %v", mm, mi)
		}
		var sgmi sgMemberInfo
		for _, v := range mi.db2sg {
			sgmi = v
			break
		}
		if len(sgmi) != 1 {
			t.Errorf("invalid member info for syncgroup member %s: %v", mm, sgmi)
		}
		expJoinerInfo := sg.Joiners[mm]
		joinerInfo := sgmi[sgId]
		if !reflect.DeepEqual(joinerInfo, expJoinerInfo) {
			t.Errorf("invalid Info for syncgroup member %s in group ID %s: got %v instead of %v",
				mm, sgId, joinerInfo, expJoinerInfo)
		}
	}

	checkSGStats(t, svc, "add-2", 1, 3)

	// Adding a syncgroup for a pre-existing name should fail.

	tx = st.NewWatchableTransaction()
	if err = s.addSyncgroup(nil, tx, NoVersion, true, "", nil, s.id, 3, 3, sg); err == nil {
		t.Errorf("re-adding the same syncgroup name %s did not fail", sgName)
	}
	tx.Abort()

	checkSGStats(t, svc, "add-3", 1, 3)

	// Fetch a non-existing syncgroup by ID or name should fail.

	badName := "not-available"
	badId := interfaces.GroupId("999")
	if sg, err := getSyncgroupByName(nil, st, badName); err == nil {
		t.Errorf("found non-existing syncgroup name %s: got %v", badName, sg)
	}
	if sg, err := getSyncgroupById(nil, st, badId); err == nil {
		t.Errorf("found non-existing syncgroup ID %s: got %v", badId, sg)
	}
}

// TestInvalidAddSyncgroup tests adding syncgroups.
func TestInvalidAddSyncgroup(t *testing.T) {
	// Set a large value to prevent the threads from firing.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	defer destroyService(t, svc)
	st := createDatabase(t, svc).St()
	s := svc.sync

	checkBadAddSyncgroup := func(t *testing.T, st *watchable.Store, sg *interfaces.Syncgroup, msg string) {
		tx := st.NewWatchableTransaction()
		if err := s.addSyncgroup(nil, tx, NoVersion, true, "", nil, s.id, 1, 1, sg); err == nil {
			t.Errorf("checkBadAddSyncgroup: adding bad syncgroup (%s) did not fail", msg)
		}
		tx.Abort()
	}

	checkBadAddSyncgroup(t, st, nil, "nil SG")

	mkSg := func() *interfaces.Syncgroup {
		return &interfaces.Syncgroup{
			Name:        "foobar",
			DbId:        mockDbId,
			Creator:     "mockCreator",
			SpecVersion: "etag-0",
			Spec: wire.SyncgroupSpec{
				Prefixes: []wire.CollectionRow{{CollectionId: makeCxId("foo"), Row: ""}, {CollectionId: makeCxId("bar"), Row: ""}},
			},
			Joiners: map[string]wire.SyncgroupMemberInfo{
				"phone":  wire.SyncgroupMemberInfo{SyncPriority: 10},
				"tablet": wire.SyncgroupMemberInfo{SyncPriority: 25},
				"cloud":  wire.SyncgroupMemberInfo{SyncPriority: 1},
			},
		}
	}

	sg := mkSg()
	sg.Name = ""
	checkBadAddSyncgroup(t, st, sg, "SG w/o name")

	sg = mkSg()
	sg.DbId = wire.Id{}
	checkBadAddSyncgroup(t, st, sg, "SG w/o DbId")

	sg = mkSg()
	sg.DbId = wire.Id{Blessing: "foo"}
	checkBadAddSyncgroup(t, st, sg, "SG with invalid (empty Name) DbId")

	sg = mkSg()
	sg.DbId = wire.Id{Name: "bar"}
	checkBadAddSyncgroup(t, st, sg, "SG with invalid (empty Blessing) DbId")

	sg = mkSg()
	sg.Creator = ""
	checkBadAddSyncgroup(t, st, sg, "SG w/o creator")

	sg = mkSg()
	sg.SpecVersion = ""
	checkBadAddSyncgroup(t, st, sg, "SG w/o Version")

	sg = mkSg()
	sg.Joiners = nil
	checkBadAddSyncgroup(t, st, sg, "SG w/o Joiners")

	sg = mkSg()
	sg.Spec.Prefixes = nil
	checkBadAddSyncgroup(t, st, sg, "SG w/o Prefixes")

	sg = mkSg()
	sg.Spec.Prefixes = []wire.CollectionRow{{CollectionId: makeCxId("foo"), Row: ""}, {CollectionId: makeCxId("bar"), Row: ""}, {CollectionId: makeCxId("foo"), Row: ""}}
	checkBadAddSyncgroup(t, st, sg, "SG with duplicate Prefixes")

	sg = mkSg()
	sg.Spec.Prefixes = []wire.CollectionRow{{CollectionId: wire.Id{}, Row: ""}}
	checkBadAddSyncgroup(t, st, sg, "SG with invalid (empty) collection id")

	sg = mkSg()
	sg.Spec.Prefixes = []wire.CollectionRow{{CollectionId: wire.Id{Blessing: "foo"}, Row: ""}}
	checkBadAddSyncgroup(t, st, sg, "SG with invalid (empty Name) collection id")

	sg = mkSg()
	sg.Spec.Prefixes = []wire.CollectionRow{{CollectionId: wire.Id{Name: "bar"}, Row: ""}}
	checkBadAddSyncgroup(t, st, sg, "SG with invalid (empty Blessing) collection id")

	sg = mkSg()
	sg.Spec.Prefixes = []wire.CollectionRow{{CollectionId: makeCxId("a"), Row: "\xfe"}}
	checkBadAddSyncgroup(t, st, sg, "SG with invalid row prefix")
}

// TestDeleteSyncgroup tests deleting a syncgroup.
func TestDeleteSyncgroup(t *testing.T) {
	// Set a large value to prevent the threads from firing.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	defer destroyService(t, svc)
	st := createDatabase(t, svc).St()
	s := svc.sync

	sgName := "foobar"
	sgId := sgNameToId(sgName)

	// Delete non-existing syncgroups.

	tx := st.NewWatchableTransaction()
	if err := delSyncgroupById(nil, nil, tx, sgId); err == nil {
		t.Errorf("deleting a non-existing syncgroup ID did not fail")
	}
	if err := delSyncgroupByName(nil, nil, tx, sgName); err == nil {
		t.Errorf("deleting a non-existing syncgroup name did not fail")
	}
	tx.Abort()

	checkSGStats(t, svc, "del-1", 0, 0)

	// Create the syncgroup to delete later.

	sg := &interfaces.Syncgroup{
		Name:        sgName,
		DbId:        mockDbId,
		Creator:     "mockCreator",
		SpecVersion: "etag-0",
		Spec: wire.SyncgroupSpec{
			Prefixes: []wire.CollectionRow{{CollectionId: makeCxId("foo"), Row: ""}, {CollectionId: makeCxId("bar"), Row: ""}},
		},
		Joiners: map[string]wire.SyncgroupMemberInfo{
			"phone":  wire.SyncgroupMemberInfo{SyncPriority: 10},
			"tablet": wire.SyncgroupMemberInfo{SyncPriority: 25},
			"cloud":  wire.SyncgroupMemberInfo{SyncPriority: 1},
		},
	}

	tx = st.NewWatchableTransaction()
	if err := s.addSyncgroup(nil, tx, NoVersion, true, "", nil, s.id, 1, 1, sg); err != nil {
		t.Errorf("creating syncgroup ID %s failed: %v", sgId, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding syncgroup ID %s: %v", sgId, err)
	}

	checkSGStats(t, svc, "del-2", 1, 3)

	// Delete it by ID.

	tx = st.NewWatchableTransaction()
	if err := delSyncgroupById(nil, nil, tx, sgId); err != nil {
		t.Errorf("deleting syncgroup ID %s failed: %v", sgId, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit deleting syncgroup ID %s: %v", sgId, err)
	}

	checkSGStats(t, svc, "del-3", 0, 0)

	// Create it again, update it, then delete it by name.

	tx = st.NewWatchableTransaction()
	if err := s.addSyncgroup(nil, tx, NoVersion, true, "", nil, s.id, 2, 2, sg); err != nil {
		t.Errorf("creating syncgroup ID %s after delete failed: %v", sgId, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding syncgroup ID %s after delete: %v", sgId, err)
	}

	tx = st.NewWatchableTransaction()
	if err := s.updateSyncgroupVersioning(nil, tx, sgId, NoVersion, true, s.id, 3, 3, sg); err != nil {
		t.Errorf("updating syncgroup ID %s version: %v", sgId, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit updating syncgroup ID %s version: %v", sgId, err)
	}

	checkSGStats(t, svc, "del-4", 1, 3)

	tx = st.NewWatchableTransaction()
	if err := delSyncgroupByName(nil, nil, tx, sgName); err != nil {
		t.Errorf("deleting syncgroup name %s failed: %v", sgName, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit deleting syncgroup name %s: %v", sgName, err)
	}

	checkSGStats(t, svc, "del-5", 0, 0)
}

// TestMultiSyncgroups tests creating multiple syncgroups.
func TestMultiSyncgroups(t *testing.T) {
	// Set a large value to prevent the threads from firing.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	defer destroyService(t, svc)
	st := createDatabase(t, svc).St()
	s := svc.sync

	sgName1, sgName2 := "foo", "bar"
	sgId1, sgId2 := sgNameToId(sgName1), sgNameToId(sgName2)

	// Add two syncgroups.

	sg1 := &interfaces.Syncgroup{
		Name:        sgName1,
		DbId:        mockDbId,
		Creator:     "mockCreator",
		SpecVersion: "etag-1",
		Spec: wire.SyncgroupSpec{
			MountTables: []string{"mt1"},
			Prefixes:    []wire.CollectionRow{{CollectionId: makeCxId("foo"), Row: ""}},
		},
		Joiners: map[string]wire.SyncgroupMemberInfo{
			"phone":  wire.SyncgroupMemberInfo{SyncPriority: 10},
			"tablet": wire.SyncgroupMemberInfo{SyncPriority: 25},
			"cloud":  wire.SyncgroupMemberInfo{SyncPriority: 1},
		},
	}
	sg2 := &interfaces.Syncgroup{
		Name:        sgName2,
		DbId:        mockDbId,
		Creator:     "mockCreator",
		SpecVersion: "etag-2",
		Spec: wire.SyncgroupSpec{
			MountTables: []string{"mt2", "mt3"},
			Prefixes:    []wire.CollectionRow{{CollectionId: makeCxId("bar"), Row: ""}},
		},
		Joiners: map[string]wire.SyncgroupMemberInfo{
			"tablet": wire.SyncgroupMemberInfo{SyncPriority: 111},
			"door":   wire.SyncgroupMemberInfo{SyncPriority: 33},
			"lamp":   wire.SyncgroupMemberInfo{SyncPriority: 9},
		},
	}

	tx := st.NewWatchableTransaction()
	if err := s.addSyncgroup(nil, tx, NoVersion, true, "", nil, s.id, 1, 1, sg1); err != nil {
		t.Errorf("creating syncgroup ID %s failed: %v", sgId1, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding syncgroup ID %s: %v", sgId1, err)
	}

	checkSGStats(t, svc, "multi-1", 1, 3)

	tx = st.NewWatchableTransaction()
	if err := s.addSyncgroup(nil, tx, NoVersion, true, "", nil, s.id, 2, 2, sg2); err != nil {
		t.Errorf("creating syncgroup ID %s failed: %v", sgId2, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding syncgroup ID %s: %v", sgId2, err)
	}

	checkSGStats(t, svc, "multi-2", 2, 5)

	// Verify membership data.

	expMembers := map[string]uint32{"phone": 1, "tablet": 2, "cloud": 1, "door": 1, "lamp": 1}

	members := svc.sync.getMembers(nil)
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid syncgroup members: got %v instead of %v", members, expMembers)
	}

	mt2and3 := map[string]struct{}{
		"mt2": struct{}{},
		"mt3": struct{}{},
	}

	expMemberInfo := map[string]*memberInfo{
		"phone": &memberInfo{
			db2sg: map[wire.Id]sgMemberInfo{
				mockDbId: sgMemberInfo{
					sgId1: sg1.Joiners["phone"],
				},
			},
			mtTables: map[string]struct{}{"mt1": struct{}{}},
		},
		"tablet": &memberInfo{
			db2sg: map[wire.Id]sgMemberInfo{
				mockDbId: sgMemberInfo{
					sgId1: sg1.Joiners["tablet"],
					sgId2: sg2.Joiners["tablet"],
				},
			},
			mtTables: map[string]struct{}{
				"mt1": struct{}{},
				"mt2": struct{}{},
				"mt3": struct{}{},
			},
		},
		"cloud": &memberInfo{
			db2sg: map[wire.Id]sgMemberInfo{
				mockDbId: sgMemberInfo{
					sgId1: sg1.Joiners["cloud"],
				},
			},
			mtTables: map[string]struct{}{"mt1": struct{}{}},
		},
		"door": &memberInfo{
			db2sg: map[wire.Id]sgMemberInfo{
				mockDbId: sgMemberInfo{
					sgId2: sg2.Joiners["door"],
				},
			},
			mtTables: mt2and3,
		},
		"lamp": &memberInfo{
			db2sg: map[wire.Id]sgMemberInfo{
				mockDbId: sgMemberInfo{
					sgId2: sg2.Joiners["lamp"],
				},
			},
			mtTables: mt2and3,
		},
	}

	view := svc.sync.allMembers
	for mm := range members {
		mi := view.members[mm]
		if mi == nil {
			t.Errorf("cannot get info for syncgroup member %s", mm)
		}
		expInfo := expMemberInfo[mm]
		if !reflect.DeepEqual(mi, expInfo) {
			t.Errorf("invalid Info for syncgroup member %s: got %#v instead of %#v", mm, mi, expInfo)
		}
	}

	// Delete the 1st syncgroup.

	tx = st.NewWatchableTransaction()
	if err := delSyncgroupById(nil, nil, tx, sgId1); err != nil {
		t.Errorf("deleting syncgroup ID %s failed: %v", sgId1, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit deleting syncgroup ID %s: %v", sgId1, err)
	}

	checkSGStats(t, svc, "multi-3", 1, 3)

	// Verify syncgroup membership data.

	expMembers = map[string]uint32{"tablet": 1, "door": 1, "lamp": 1}

	members = svc.sync.getMembers(nil)
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid syncgroup members: got %v instead of %v", members, expMembers)
	}

	expMemberInfo = map[string]*memberInfo{
		"tablet": &memberInfo{
			db2sg: map[wire.Id]sgMemberInfo{
				mockDbId: sgMemberInfo{
					sgId2: sg2.Joiners["tablet"],
				},
			},
			mtTables: mt2and3,
		},
		"door": &memberInfo{
			db2sg: map[wire.Id]sgMemberInfo{
				mockDbId: sgMemberInfo{
					sgId2: sg2.Joiners["door"],
				},
			},
			mtTables: mt2and3,
		},
		"lamp": &memberInfo{
			db2sg: map[wire.Id]sgMemberInfo{
				mockDbId: sgMemberInfo{
					sgId2: sg2.Joiners["lamp"],
				},
			},
			mtTables: mt2and3,
		},
	}

	view = svc.sync.allMembers
	for mm := range members {
		mi := view.members[mm]
		if mi == nil {
			t.Errorf("cannot get info for syncgroup member %s", mm)
		}
		expInfo := expMemberInfo[mm]
		if !reflect.DeepEqual(mi, expInfo) {
			t.Errorf("invalid Info for syncgroup member %s: got %v instead of %v", mm, mi, expInfo)
		}
	}
}

// TestPrefixCompare tests the prefix comparison utility.
func TestPrefixCompare(t *testing.T) {
	mksgps := func(strs []string) []wire.CollectionRow {
		res := make([]wire.CollectionRow, len(strs))
		for i, v := range strs {
			parts := strings.SplitN(v, ":", 2)
			if len(parts) != 2 {
				t.Fatalf("invalid CollectionRow string: %s", v)
			}
			res[i] = wire.CollectionRow{CollectionId: wire.Id{"u", parts[0]}, Row: parts[1]}
		}
		return res
	}

	check := func(t *testing.T, strs1, strs2 []string, want bool, msg string) {
		if got := samePrefixes(mksgps(strs1), mksgps(strs2)); got != want {
			t.Errorf("samePrefixes: %s: got %t instead of %t", msg, got, want)
		}
	}

	check(t, nil, nil, true, "both nil")
	check(t, []string{}, nil, true, "empty vs nil")
	check(t, []string{"a:", "b:"}, []string{"b:", "a:"}, true, "different ordering")
	check(t, []string{"a:", "b:", "c:"}, []string{"b:", "a:"}, false, "p1 superset of p2")
	check(t, []string{"a:", "b:"}, []string{"b:", "a:", "c:"}, false, "p2 superset of p1")
	check(t, []string{"a:", "b:", "c:"}, []string{"b:", "d:", "a:"}, false, "overlap")
	check(t, []string{"a:", "b:", "c:"}, []string{"x:", "y:"}, false, "no overlap")
	check(t, []string{"a:", "b:"}, []string{"B:", "a:"}, false, "upper/lowercases")
	check(t, []string{"a:b", "b:c"}, []string{"b:c", "a:b"}, true, "different ordering, with non-empty row prefixes")
	check(t, []string{"a:b"}, []string{"a:b", "a:c"}, false, "p2 superset, with non-empty row prefixes")
}
