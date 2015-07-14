// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for SyncGroup management and storage in Syncbase.

import (
	"reflect"
	"testing"
	"time"

	"v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/store"
	_ "v.io/x/ref/runtime/factories/generic"
)

// checkSGStats verifies SyncGroup stats.
func checkSGStats(t *testing.T, svc *mockService, which string, numSG, numMembers int) {
	memberViewTTL = 0 // Always recompute the SyncGroup membership view.
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

// TestAddSyncGroup tests adding SyncGroups.
func TestAddSyncGroup(t *testing.T) {
	// Set a large value to prevent the initiator from running. Since this
	// test adds a fake SyncGroup, if the initiator runs, it will attempt
	// to initiate using this fake and partial SyncGroup data.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()

	checkSGStats(t, svc, "add-1", 0, 0)

	// Add a SyncGroup.

	sgName := "foobar"
	sgId := interfaces.GroupId(1234)

	sg := &interfaces.SyncGroup{
		Name:        sgName,
		Id:          sgId,
		AppName:     "mockApp",
		DbName:      "mockDB",
		Creator:     "mockCreator",
		SpecVersion: "etag-0",
		Spec: nosql.SyncGroupSpec{
			Prefixes: []string{"foo", "bar"},
		},
		Joiners: map[string]nosql.SyncGroupMemberInfo{
			"phone":  nosql.SyncGroupMemberInfo{SyncPriority: 10},
			"tablet": nosql.SyncGroupMemberInfo{SyncPriority: 25},
			"cloud":  nosql.SyncGroupMemberInfo{SyncPriority: 1},
		},
	}

	tx := st.NewTransaction()
	if err := addSyncGroup(nil, tx, sg); err != nil {
		t.Errorf("cannot add SyncGroup ID %d: %v", sg.Id, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding SyncGroup ID %d: %v", sg.Id, err)
	}

	// Verify SyncGroup ID, name, and data.

	if id, err := getSyncGroupId(nil, st, sgName); err != nil || id != sgId {
		t.Errorf("cannot get ID of SyncGroup %s: got %d instead of %d; err: %v", sgName, id, sgId, err)
	}
	if name, err := getSyncGroupName(nil, st, sgId); err != nil || name != sgName {
		t.Errorf("cannot get name of SyncGroup %d: got %s instead of %s; err: %v",
			sgId, name, sgName, err)
	}

	sgOut, err := getSyncGroupById(nil, st, sgId)
	if err != nil {
		t.Errorf("cannot get SyncGroup by ID %d: %v", sgId, err)
	}
	if !reflect.DeepEqual(sgOut, sg) {
		t.Errorf("invalid SyncGroup data for group ID %d: got %v instead of %v", sgId, sgOut, sg)
	}

	sgOut, err = getSyncGroupByName(nil, st, sgName)
	if err != nil {
		t.Errorf("cannot get SyncGroup by Name %s: %v", sgName, err)
	}
	if !reflect.DeepEqual(sgOut, sg) {
		t.Errorf("invalid SyncGroup data for group name %s: got %v instead of %v", sgName, sgOut, sg)
	}

	// Verify membership data.

	expMembers := map[string]uint32{"phone": 1, "tablet": 1, "cloud": 1}

	members := svc.sync.getMembers(nil)
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid SyncGroup members: got %v instead of %v", members, expMembers)
	}

	view := svc.sync.allMembers
	for mm := range members {
		mi := view.members[mm]
		if mi == nil {
			t.Errorf("cannot get info for SyncGroup member %s", mm)
		}
		if len(mi.db2sg) != 1 {
			t.Errorf("invalid info for SyncGroup member %s: %v", mm, mi)
		}
		var sgmi sgMemberInfo
		for _, v := range mi.db2sg {
			sgmi = v
			break
		}
		if len(sgmi) != 1 {
			t.Errorf("invalid member info for SyncGroup member %s: %v", mm, sgmi)
		}
		expJoinerInfo := sg.Joiners[mm]
		joinerInfo := sgmi[sgId]
		if !reflect.DeepEqual(joinerInfo, expJoinerInfo) {
			t.Errorf("invalid Info for SyncGroup member %s in group ID %d: got %v instead of %v",
				mm, sgId, joinerInfo, expJoinerInfo)
		}
	}

	checkSGStats(t, svc, "add-2", 1, 3)

	// Adding a SyncGroup for a pre-existing group ID or name should fail.

	sg.Name = "another-name"

	tx = st.NewTransaction()
	if err = addSyncGroup(nil, tx, sg); err == nil {
		t.Errorf("re-adding SyncGroup %d did not fail", sgId)
	}
	tx.Abort()

	sg.Name = sgName
	sg.Id = interfaces.GroupId(5555)

	tx = st.NewTransaction()
	if err = addSyncGroup(nil, tx, sg); err == nil {
		t.Errorf("adding SyncGroup %s with a different ID did not fail", sgName)
	}
	tx.Abort()

	checkSGStats(t, svc, "add-3", 1, 3)

	// Fetch a non-existing SyncGroup by ID or name should fail.

	badName := "not-available"
	badId := interfaces.GroupId(999)
	if id, err := getSyncGroupId(nil, st, badName); err == nil {
		t.Errorf("found non-existing SyncGroup %s: got ID %d", badName, id)
	}
	if name, err := getSyncGroupName(nil, st, badId); err == nil {
		t.Errorf("found non-existing SyncGroup %d: got name %s", badId, name)
	}
	if sg, err := getSyncGroupByName(nil, st, badName); err == nil {
		t.Errorf("found non-existing SyncGroup %s: got %v", badName, sg)
	}
	if sg, err := getSyncGroupById(nil, st, badId); err == nil {
		t.Errorf("found non-existing SyncGroup %d: got %v", badId, sg)
	}
}

// TestInvalidAddSyncGroup tests adding SyncGroups.
func TestInvalidAddSyncGroup(t *testing.T) {
	// Set a large value to prevent the threads from firing.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()

	checkBadAddSyncGroup := func(t *testing.T, st store.Store, sg *interfaces.SyncGroup, msg string) {
		tx := st.NewTransaction()
		if err := addSyncGroup(nil, tx, sg); err == nil {
			t.Errorf("checkBadAddSyncGroup: adding bad SyncGroup (%s) did not fail", msg)
		}
		tx.Abort()
	}

	checkBadAddSyncGroup(t, st, nil, "nil SG")

	sg := &interfaces.SyncGroup{Id: 1234}
	checkBadAddSyncGroup(t, st, sg, "SG w/o name")

	sg = &interfaces.SyncGroup{Name: "foobar"}
	checkBadAddSyncGroup(t, st, sg, "SG w/o Id")

	sg.Id = 1234
	checkBadAddSyncGroup(t, st, sg, "SG w/o Version")

	sg.SpecVersion = "v1"
	checkBadAddSyncGroup(t, st, sg, "SG w/o Joiners")

	sg.Joiners = map[string]nosql.SyncGroupMemberInfo{
		"phone": nosql.SyncGroupMemberInfo{SyncPriority: 10},
	}
	checkBadAddSyncGroup(t, st, sg, "SG w/o Prefixes")
}

// TestDeleteSyncGroup tests deleting a SyncGroup.
func TestDeleteSyncGroup(t *testing.T) {
	// Set a large value to prevent the threads from firing.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()

	sgName := "foobar"
	sgId := interfaces.GroupId(1234)

	// Delete non-existing SyncGroups.

	tx := st.NewTransaction()
	if err := delSyncGroupById(nil, tx, sgId); err == nil {
		t.Errorf("deleting a non-existing SyncGroup ID did not fail")
	}
	if err := delSyncGroupByName(nil, tx, sgName); err == nil {
		t.Errorf("deleting a non-existing SyncGroup name did not fail")
	}
	tx.Abort()

	checkSGStats(t, svc, "del-1", 0, 0)

	// Create the SyncGroup to delete later.

	sg := &interfaces.SyncGroup{
		Name:        sgName,
		Id:          sgId,
		AppName:     "mockApp",
		DbName:      "mockDB",
		Creator:     "mockCreator",
		SpecVersion: "etag-0",
		Spec: nosql.SyncGroupSpec{
			Prefixes: []string{"foo", "bar"},
		},
		Joiners: map[string]nosql.SyncGroupMemberInfo{
			"phone":  nosql.SyncGroupMemberInfo{SyncPriority: 10},
			"tablet": nosql.SyncGroupMemberInfo{SyncPriority: 25},
			"cloud":  nosql.SyncGroupMemberInfo{SyncPriority: 1},
		},
	}

	tx = st.NewTransaction()
	if err := addSyncGroup(nil, tx, sg); err != nil {
		t.Errorf("creating SyncGroup ID %d failed: %v", sgId, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding SyncGroup ID %d: %v", sgId, err)
	}

	checkSGStats(t, svc, "del-2", 1, 3)

	// Delete it by ID.

	tx = st.NewTransaction()
	if err := delSyncGroupById(nil, tx, sgId); err != nil {
		t.Errorf("deleting SyncGroup ID %d failed: %v", sgId, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit deleting SyncGroup ID %d: %v", sgId, err)
	}

	checkSGStats(t, svc, "del-3", 0, 0)

	// Create it again then delete it by name.

	tx = st.NewTransaction()
	if err := addSyncGroup(nil, tx, sg); err != nil {
		t.Errorf("creating SyncGroup ID %d after delete failed: %v", sgId, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding SyncGroup ID %d after delete: %v", sgId, err)
	}

	checkSGStats(t, svc, "del-4", 1, 3)

	tx = st.NewTransaction()
	if err := delSyncGroupByName(nil, tx, sgName); err != nil {
		t.Errorf("deleting SyncGroup name %s failed: %v", sgName, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit deleting SyncGroup name %s: %v", sgName, err)
	}

	checkSGStats(t, svc, "del-5", 0, 0)
}

// TestMultiSyncGroups tests creating multiple SyncGroups.
func TestMultiSyncGroups(t *testing.T) {
	// Set a large value to prevent the threads from firing.
	peerSyncInterval = 1 * time.Hour
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()

	sgName1, sgName2 := "foo", "bar"
	sgId1, sgId2 := interfaces.GroupId(1234), interfaces.GroupId(8888)

	// Add two SyncGroups.

	sg1 := &interfaces.SyncGroup{
		Name:        sgName1,
		Id:          sgId1,
		AppName:     "mockApp",
		DbName:      "mockDB",
		Creator:     "mockCreator",
		SpecVersion: "etag-1",
		Spec: nosql.SyncGroupSpec{
			Prefixes: []string{"foo"},
		},
		Joiners: map[string]nosql.SyncGroupMemberInfo{
			"phone":  nosql.SyncGroupMemberInfo{SyncPriority: 10},
			"tablet": nosql.SyncGroupMemberInfo{SyncPriority: 25},
			"cloud":  nosql.SyncGroupMemberInfo{SyncPriority: 1},
		},
	}
	sg2 := &interfaces.SyncGroup{
		Name:        sgName2,
		Id:          sgId2,
		AppName:     "mockApp",
		DbName:      "mockDB",
		Creator:     "mockCreator",
		SpecVersion: "etag-2",
		Spec: nosql.SyncGroupSpec{
			Prefixes: []string{"bar"},
		},
		Joiners: map[string]nosql.SyncGroupMemberInfo{
			"tablet": nosql.SyncGroupMemberInfo{SyncPriority: 111},
			"door":   nosql.SyncGroupMemberInfo{SyncPriority: 33},
			"lamp":   nosql.SyncGroupMemberInfo{SyncPriority: 9},
		},
	}

	tx := st.NewTransaction()
	if err := addSyncGroup(nil, tx, sg1); err != nil {
		t.Errorf("creating SyncGroup ID %d failed: %v", sgId1, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding SyncGroup ID %d: %v", sgId1, err)
	}

	checkSGStats(t, svc, "multi-1", 1, 3)

	tx = st.NewTransaction()
	if err := addSyncGroup(nil, tx, sg2); err != nil {
		t.Errorf("creating SyncGroup ID %d failed: %v", sgId2, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit adding SyncGroup ID %d: %v", sgId2, err)
	}

	checkSGStats(t, svc, "multi-2", 2, 5)

	// Verify membership data.

	expMembers := map[string]uint32{"phone": 1, "tablet": 2, "cloud": 1, "door": 1, "lamp": 1}

	members := svc.sync.getMembers(nil)
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid SyncGroup members: got %v instead of %v", members, expMembers)
	}

	expMemberInfo := map[string]*memberInfo{
		"phone": &memberInfo{
			db2sg: map[string]sgMemberInfo{
				"mockapp:mockdb": sgMemberInfo{
					sgId1: sg1.Joiners["phone"],
				},
			},
		},
		"tablet": &memberInfo{
			db2sg: map[string]sgMemberInfo{
				"mockapp:mockdb": sgMemberInfo{
					sgId1: sg1.Joiners["tablet"],
					sgId2: sg2.Joiners["tablet"],
				},
			},
		},
		"cloud": &memberInfo{
			db2sg: map[string]sgMemberInfo{
				"mockapp:mockdb": sgMemberInfo{
					sgId1: sg1.Joiners["cloud"],
				},
			},
		},
		"door": &memberInfo{
			db2sg: map[string]sgMemberInfo{
				"mockapp:mockdb": sgMemberInfo{
					sgId2: sg2.Joiners["door"],
				},
			},
		},
		"lamp": &memberInfo{
			db2sg: map[string]sgMemberInfo{
				"mockapp:mockdb": sgMemberInfo{
					sgId2: sg2.Joiners["lamp"],
				},
			},
		},
	}

	view := svc.sync.allMembers
	for mm := range members {
		mi := view.members[mm]
		if mi == nil {
			t.Errorf("cannot get info for SyncGroup member %s", mm)
		}
		expInfo := expMemberInfo[mm]
		if !reflect.DeepEqual(mi, expInfo) {
			t.Errorf("invalid Info for SyncGroup member %s: got %v instead of %v", mm, mi, expInfo)
		}
	}

	// Delete the 1st SyncGroup.

	tx = st.NewTransaction()
	if err := delSyncGroupById(nil, tx, sgId1); err != nil {
		t.Errorf("deleting SyncGroup ID %d failed: %v", sgId1, err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("cannot commit deleting SyncGroup ID %d: %v", sgId1, err)
	}

	checkSGStats(t, svc, "multi-3", 1, 3)

	// Verify SyncGroup membership data.

	expMembers = map[string]uint32{"tablet": 1, "door": 1, "lamp": 1}

	members = svc.sync.getMembers(nil)
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid SyncGroup members: got %v instead of %v", members, expMembers)
	}

	expMemberInfo = map[string]*memberInfo{
		"tablet": &memberInfo{
			db2sg: map[string]sgMemberInfo{
				"mockapp:mockdb": sgMemberInfo{
					sgId2: sg2.Joiners["tablet"],
				},
			},
		},
		"door": &memberInfo{
			db2sg: map[string]sgMemberInfo{
				"mockapp:mockdb": sgMemberInfo{
					sgId2: sg2.Joiners["door"],
				},
			},
		},
		"lamp": &memberInfo{
			db2sg: map[string]sgMemberInfo{
				"mockapp:mockdb": sgMemberInfo{
					sgId2: sg2.Joiners["lamp"],
				},
			},
		},
	}

	view = svc.sync.allMembers
	for mm := range members {
		mi := view.members[mm]
		if mi == nil {
			t.Errorf("cannot get info for SyncGroup member %s", mm)
		}
		expInfo := expMemberInfo[mm]
		if !reflect.DeepEqual(mi, expInfo) {
			t.Errorf("invalid Info for SyncGroup member %s: got %v instead of %v", mm, mi, expInfo)
		}
	}
}
