// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for the Veyron SyncGroup Table.

import (
	"os"
	"reflect"
	"testing"

	"v.io/x/ref/lib/stats"
)

// TestSyncGroupTableOpen tests the creation of a SyncGroup Table, closing and re-opening it.
// It also verifies that its backing file is created and that a 2nd close is safe.
func TestSyncGroupTableOpen(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	fsize := getFileSize(sgfile)
	if fsize < 0 {
		//t.Fatalf("SyncGroup Table file %s not created", sgfile)
	}

	sg.flush()
	oldfsize := fsize
	fsize = getFileSize(sgfile)
	if fsize <= oldfsize {
		//t.Fatalf("SyncGroup Table file %s not flushed", sgfile)
	}

	sg.close()

	sg, err = openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot re-open existing SyncGroup Table file %s", sgfile)
	}

	oldfsize = fsize
	fsize = getFileSize(sgfile)
	if fsize != oldfsize {
		t.Fatalf("SyncGroup Table file %s size changed across re-open", sgfile)
	}

	sg.close()
	sg.close() // multiple closes should be a safe NOP

	fsize = getFileSize(sgfile)
	if fsize != oldfsize {
		t.Fatalf("SyncGroup Table file %s size changed across close", sgfile)
	}

	// Fail opening a SyncGroup Table in a non-existent directory.
	_, err = openSyncGroupTable("/not/really/there/junk.sg")
	if err == nil {
		//t.Fatalf("openSyncGroupTable() did not fail when using a bad pathname")
	}
}

// TestInvalidSyncGroupTable tests using methods on an invalid (closed) SyncGroup Table.
func TestInvalidSyncGroupTable(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	sg.close()

	sgid, err := strToGroupId("1234")
	if err != nil {
		t.Error(err)
	}

	validateError := func(t *testing.T, err error, funcName string) {
		if err == nil || err.Error() != "invalid SyncGroup Table" {
			t.Errorf("%s() did not fail on a closed SyncGroup Table: %v", funcName, err)
		}
	}

	err = sg.addSyncGroup(&syncGroupData{})
	validateError(t, err, "addSyncGroup")

	_, err = sg.getSyncGroupID("foobar")
	validateError(t, err, "getSyncGroupID")

	_, err = sg.getSyncGroupName(sgid)
	validateError(t, err, "getSyncGroupName")

	_, err = sg.getSyncGroupByID(sgid)
	validateError(t, err, "getSyncGroupByID")

	_, err = sg.getSyncGroupByName("foobar")
	validateError(t, err, "getSyncGroupByName")

	err = sg.updateSyncGroup(&syncGroupData{})
	validateError(t, err, "updateSyncGroup")

	err = sg.delSyncGroupByID(sgid)
	validateError(t, err, "delSyncGroupByID")

	err = sg.delSyncGroupByName("foobar")
	validateError(t, err, "delSyncGroupByName")

	_, err = sg.getAllSyncGroupNames()
	validateError(t, err, "getAllSyncGroupNames")

	_, err = sg.getMembers()
	validateError(t, err, "getMembers")

	_, err = sg.getMemberInfo("foobar")
	validateError(t, err, "getMemberInfo")

	err = sg.setSGDataEntry(sgid, &syncGroupData{})
	validateError(t, err, "setSGDataEntry")

	_, err = sg.getSGDataEntry(sgid)
	validateError(t, err, "getSGDataEntry")

	err = sg.delSGDataEntry(sgid)
	validateError(t, err, "delSGDataEntry")

	err = sg.setSGNameEntry("foobar", sgid)
	validateError(t, err, "setSGNameEntry")

	_, err = sg.getSGNameEntry("foobar")
	validateError(t, err, "getSGNameEntry")

	err = sg.delSGNameEntry("foobar")
	validateError(t, err, "delSGNameEntry")

	// These calls should be harmless NOPs.
	sg.dump()
	sg.flush()
	sg.close()
	sg.addMember("foobar", sgid, JoinerMetaData{})
	sg.delMember("foobar", sgid)
	sg.addAllMembers(&syncGroupData{})
	sg.delAllMembers(&syncGroupData{})

	if sg.hasSGDataEntry(sgid) {
		t.Errorf("hasSGDataEntry() found an entry on a closed SyncGroup Table")
	}
	if sg.hasSGNameEntry("foobar") {
		t.Errorf("hasSGNameEntry() found an entry on a closed SyncGroup Table")
	}
}

// checkSGStats verifies the SyncGroup Table stats counters.
func checkSGStats(t *testing.T, which string, numSG, numMembers int64) {
	if num, err := stats.Value(statsNumSyncGroup); err != nil || num != numSG {
		t.Errorf("num-syncgroups (%s): got %v (err: %v) instead of %v", which, num, err, numSG)
	}
	if num, err := stats.Value(statsNumMember); err != nil || num != numMembers {
		t.Errorf("num-members (%s): got %v  (err: %v) instead of %v", which, num, err, numMembers)
	}
}

// TestAddSyncGroup tests adding SyncGroups.
func TestAddSyncGroup(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	checkSGStats(t, "add-1", 0, 0)

	sgname := "foobar"
	sgid, err := strToGroupId("1234")
	if err != nil {
		t.Fatal(err)
	}

	sgData := &syncGroupData{
		SrvInfo: SyncGroupInfo{
			Id:        sgid,
			GroupName: sgname,
			Joiners: map[string]JoinerMetaData{
				"phone":  JoinerMetaData{SyncPriority: 10},
				"tablet": JoinerMetaData{SyncPriority: 25},
				"cloud":  JoinerMetaData{SyncPriority: 1},
			},
		},
		LocalPath: "/foo/bar",
	}

	err = sg.addSyncGroup(sgData)
	if err != nil {
		t.Errorf("adding SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	// Verify SyncGroup ID, name, and data.
	if id, err := sg.getSyncGroupID(sgname); err != nil || id != sgid {
		t.Errorf("cannot get back ID of SyncGroup %s: got ID %d instead of %d; err: %v", sgname, id, sgid, err)
	}
	if name, err := sg.getSyncGroupName(sgid); err != nil || name != sgname {
		t.Errorf("cannot get back name of SyncGroup ID %d: got %s instead of %s; err: %v", sgid, name, sgname, err)
	}

	data, err := sg.getSyncGroupByID(sgid)
	if err != nil {
		t.Errorf("cannot get SyncGroup by ID %d: %v", sgid, err)
	}
	if !reflect.DeepEqual(data, sgData) {
		t.Errorf("invalid SyncGroup data for group ID %d: got %v instead of %v", sgid, data, sgData)
	}

	data, err = sg.getSyncGroupByName(sgname)
	if err != nil {
		t.Errorf("cannot get SyncGroup by Name %s: %v", sgname, err)
	}
	if !reflect.DeepEqual(data, sgData) {
		t.Errorf("invalid SyncGroup data for group name %s: got %v instead of %v", sgname, data, sgData)
	}

	// Verify membership data.
	members, err := sg.getMembers()
	if err != nil {
		t.Errorf("cannot get all SyncGroup members: %v", err)
	}
	expMembers := map[string]uint32{"phone": 1, "tablet": 1, "cloud": 1}
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid SyncGroup members: got %v instead of %v", members, expMembers)
	}

	expMetaData := map[string]*memberMetaData{
		"phone":  &memberMetaData{metaData: JoinerMetaData{SyncPriority: 10}},
		"tablet": &memberMetaData{metaData: JoinerMetaData{SyncPriority: 25}},
		"cloud":  &memberMetaData{metaData: JoinerMetaData{SyncPriority: 1}},
	}
	for mm := range members {
		info, err := sg.getMemberInfo(mm)
		if err != nil || info == nil {
			t.Errorf("cannot get info for SyncGroup member %s: info: %v, err: %v", mm, info, err)
		}
		if len(info.gids) != 1 {
			t.Errorf("invalid info for SyncGroup member %s: %v", mm, info)
		}
		expJoinerMetaData := expMetaData[mm]
		joinerMetaData := info.gids[sgid]
		if !reflect.DeepEqual(joinerMetaData, expJoinerMetaData) {
			t.Errorf("invalid joiner Data for SyncGroup member %s under group ID %d: got %v instead of %v",
				mm, sgid, joinerMetaData, expJoinerMetaData)
		}
	}

	checkSGStats(t, "add-2", 1, 3)

	// Use a non-existent member.
	if info, err := sg.getMemberInfo("should-not-be-there"); err == nil {
		t.Errorf("found info for invalid SyncGroup member: %v", info)
	}

	// Adding a SyncGroup for a pre-existing group ID or name should fail.
	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("re-adding SyncGroup %d did not fail", sgid)
	}

	sgData.SrvInfo.Id, err = strToGroupId("5555")
	if err != nil {
		t.Fatal(err)
	}
	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("adding SyncGroup %s with a different ID did not fail", sgname)
	}

	checkSGStats(t, "add-3", 1, 3)

	sg.dump()
	sg.close()
}

// TestInvalidAddSyncGroup tests adding SyncGroups.
func TestInvalidAddSyncGroup(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	sgname := "foobar"
	sgid, err := strToGroupId("1234")
	if err != nil {
		t.Fatal(err)
	}

	err = sg.addSyncGroup(nil)
	if err == nil {
		t.Errorf("adding a nil SyncGroup did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData := &syncGroupData{}
	sgData.SrvInfo.Id = sgid

	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("adding a SyncGroup with an empty name did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData.SrvInfo.GroupName = sgname

	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("adding a SyncGroup with no local path did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData.LocalPath = "/foo/bar"

	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("adding a SyncGroup with no joiners did not fail in SyncGroup Table file %s", sgfile)
	}

	sg.dump()
	sg.close()
}

// TestUpdateSyncGroup tests updating a SyncGroup.
func TestUpdateSyncGroup(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	err = sg.updateSyncGroup(nil)
	if err == nil {
		t.Errorf("updating a nil SyncGroup did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData := &syncGroupData{}
	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with an empty name did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData.SrvInfo.GroupName = "blabla"
	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with no joiners did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData.SrvInfo.Joiners = map[string]JoinerMetaData{
		"phone": JoinerMetaData{SyncPriority: 10},
	}
	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with a non-existing name did not fail in SyncGroup Table file %s", sgfile)
	}

	// Create the SyncGroup to update later.
	sgname := "foobar"
	sgid, err := strToGroupId("1234")
	if err != nil {
		t.Fatal(err)
	}

	sgData = &syncGroupData{
		SrvInfo: SyncGroupInfo{
			Id:        sgid,
			GroupName: sgname,
			Joiners: map[string]JoinerMetaData{
				"phone":  JoinerMetaData{SyncPriority: 10},
				"tablet": JoinerMetaData{SyncPriority: 25},
				"cloud":  JoinerMetaData{SyncPriority: 1},
			},
		},
		LocalPath: "/foo/bar",
	}

	err = sg.addSyncGroup(sgData)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	checkSGStats(t, "up-1", 1, 3)

	// Update it using different group or root IDs, which is not allowed.
	xid, err := strToGroupId("9999")
	if err != nil {
		t.Fatal(err)
	}

	sgData.SrvInfo.Id = xid

	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with an ID mismatch did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData.SrvInfo.Id = sgid
	sgData.LocalPath = "hahahaha"
	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with a local path mismatch did not fail in SyncGroup Table file %s", sgfile)
	}

	checkSGStats(t, "up-2", 1, 3)

	// Update it using a modified set of joiners.
	// An empty string indicates no change to the local path.
	sgData.LocalPath = ""
	sgData.SrvInfo.Joiners["universe"] = JoinerMetaData{SyncPriority: 0}
	delete(sgData.SrvInfo.Joiners, "cloud")

	err = sg.updateSyncGroup(sgData)
	if err != nil {
		t.Errorf("updating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	// Do some NOP member deletions (bad member, bad group ID).
	// SyncGroup verification (below) should see the expected info asserting these were NOPs.
	sg.delMember("blablablablabla", sgid)
	sg.delMember("phone", xid)

	checkSGStats(t, "up-3", 1, 3)

	// Verify updated SyncGroup.
	if id, err := sg.getSyncGroupID(sgname); err != nil || id != sgid {
		t.Errorf("cannot get back ID of updated SyncGroup %s: got ID %d instead of %d; err: %v", sgname, id, sgid, err)
	}
	if name, err := sg.getSyncGroupName(sgid); err != nil || name != sgname {
		t.Errorf("cannot get back name of updated SyncGroup ID %d: got %s instead of %s; err: %v", sgid, name, sgname, err)
	}

	expData := &syncGroupData{
		SrvInfo: SyncGroupInfo{
			Id:        sgid,
			GroupName: sgname,
			Joiners: map[string]JoinerMetaData{
				"phone":    JoinerMetaData{SyncPriority: 10},
				"tablet":   JoinerMetaData{SyncPriority: 25},
				"universe": JoinerMetaData{SyncPriority: 0},
			},
		},
		LocalPath: "/foo/bar",
	}

	data, err := sg.getSyncGroupByID(sgid)
	if err != nil {
		t.Errorf("cannot get updated SyncGroup by ID %d: %v", sgid, err)
	}
	if !reflect.DeepEqual(data, expData) {
		t.Errorf("invalid SyncGroup data for updated group ID %d: got %v instead of %v", sgid, data, expData)
	}

	data, err = sg.getSyncGroupByName(sgname)
	if err != nil {
		t.Errorf("cannot get updated SyncGroup by Name %s: %v", sgname, err)
	}
	if !reflect.DeepEqual(data, expData) {
		t.Errorf("invalid SyncGroup data for updated group name %s: got %v instead of %v", sgname, data, expData)
	}

	// Verify membership data.
	members, err := sg.getMembers()
	if err != nil {
		t.Errorf("cannot get all SyncGroup members after update: %v", err)
	}
	expMembers := map[string]uint32{"phone": 1, "tablet": 1, "universe": 1}
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid SyncGroup members after update: got %v instead of %v", members, expMembers)
	}

	expMetaData := map[string]*memberMetaData{
		"phone":    &memberMetaData{metaData: JoinerMetaData{SyncPriority: 10}},
		"tablet":   &memberMetaData{metaData: JoinerMetaData{SyncPriority: 25}},
		"universe": &memberMetaData{metaData: JoinerMetaData{SyncPriority: 0}},
	}
	for mm := range members {
		info, err := sg.getMemberInfo(mm)
		if err != nil || info == nil {
			t.Errorf("cannot get info for SyncGroup member %s: info: %v, err: %v", mm, info, err)
		}
		if len(info.gids) != 1 {
			t.Errorf("invalid info for SyncGroup member %s: %v", mm, info)
		}
		expJoinerMetaData := expMetaData[mm]
		joinerMetaData := info.gids[sgid]
		if !reflect.DeepEqual(joinerMetaData, expJoinerMetaData) {
			t.Errorf("invalid joiner Data for SyncGroup member %s under group ID %d: got %v instead of %v",
				mm, sgid, joinerMetaData, expJoinerMetaData)
		}
	}

	sg.dump()
	sg.close()
}

// TestDeleteSyncGroup tests deleting a SyncGroup.
func TestDeleteSyncGroup(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	sgname := "foobar"
	sgid, err := strToGroupId("1234")
	if err != nil {
		t.Fatal(err)
	}

	// Delete non-existing SyncGroups.
	err = sg.delSyncGroupByID(sgid)
	if err == nil {
		t.Errorf("deleting a non-existing SyncGroup ID did not fail in SyncGroup Table file %s", sgfile)
	}

	err = sg.delSyncGroupByName(sgname)
	if err == nil {
		t.Errorf("deleting a non-existing SyncGroup name did not fail in SyncGroup Table file %s", sgfile)
	}

	checkSGStats(t, "del-1", 0, 0)

	// Create the SyncGroup to delete later.
	sgData := &syncGroupData{
		SrvInfo: SyncGroupInfo{
			Id:        sgid,
			GroupName: sgname,
			Joiners: map[string]JoinerMetaData{
				"phone":  JoinerMetaData{SyncPriority: 10},
				"tablet": JoinerMetaData{SyncPriority: 25},
				"cloud":  JoinerMetaData{SyncPriority: 1},
			},
		},
		LocalPath: "/foo/bar",
	}

	err = sg.addSyncGroup(sgData)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	checkSGStats(t, "del-2", 1, 3)

	// Delete it by ID.
	err = sg.delSyncGroupByID(sgid)
	if err != nil {
		t.Errorf("deleting SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	checkSGStats(t, "del-3", 0, 0)

	// Create it again then delete it by name.
	err = sg.addSyncGroup(sgData)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	checkSGStats(t, "del-4", 1, 3)

	err = sg.delSyncGroupByName(sgname)
	if err != nil {
		t.Errorf("deleting SyncGroup name %s failed in SyncGroup Table file %s: %v", sgname, sgfile, err)
	}

	checkSGStats(t, "del-5", 0, 0)

	sg.dump()
	sg.close()
}

// TestMultiSyncGroups tests creating multiple SyncGroups.
func TestMultiSyncGroups(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	sgname1, sgname2 := "foo", "bar"
	sgid1, err := strToGroupId("1234")
	if err != nil {
		t.Fatal(err)
	}
	sgid2, err := strToGroupId("8888")
	if err != nil {
		t.Fatal(err)
	}

	// Add two SyncGroups.
	sgData1 := &syncGroupData{
		SrvInfo: SyncGroupInfo{
			Id:        sgid1,
			GroupName: sgname1,
			Joiners: map[string]JoinerMetaData{
				"phone":  JoinerMetaData{SyncPriority: 10},
				"tablet": JoinerMetaData{SyncPriority: 25},
				"cloud":  JoinerMetaData{SyncPriority: 1},
			},
		},
		LocalPath: "/foo/bar",
	}

	sgData2 := &syncGroupData{
		SrvInfo: SyncGroupInfo{
			Id:        sgid2,
			GroupName: sgname2,
			Joiners: map[string]JoinerMetaData{
				"tablet": JoinerMetaData{SyncPriority: 111},
				"door":   JoinerMetaData{SyncPriority: 33},
				"lamp":   JoinerMetaData{SyncPriority: 9},
			},
		},
		LocalPath: "/foo/bar",
	}

	err = sg.addSyncGroup(sgData1)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid1, sgfile, err)
	}

	checkSGStats(t, "multi-1", 1, 3)

	err = sg.addSyncGroup(sgData2)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid2, sgfile, err)
	}

	checkSGStats(t, "multi-2", 2, 5)

	// Verify SyncGroup names.
	sgNames, err := sg.getAllSyncGroupNames()
	if err != nil {
		t.Errorf("cannot get all SyncGroup names: %v", err)
	}
	if len(sgNames) != 2 {
		t.Errorf("wrong number of SyncGroup names: %d instead of 2", len(sgNames))
	}
	expNames := map[string]struct{}{sgname1: struct{}{}, sgname2: struct{}{}}
	for _, name := range sgNames {
		if _, ok := expNames[name]; !ok {
			t.Errorf("unknown SyncGroup name returned: %s", name)
		} else {
			delete(expNames, name)
		}
	}

	if len(expNames) > 0 {
		t.Errorf("SyncGroup names missing, not returned: %v", expNames)
	}

	// Verify SyncGroup membership data.
	members, err := sg.getMembers()
	if err != nil {
		t.Errorf("cannot get all SyncGroup members: %v", err)
	}

	expMembers := map[string]uint32{"phone": 1, "tablet": 2, "cloud": 1, "door": 1, "lamp": 1}
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid SyncGroup members: got %v instead of %v", members, expMembers)
	}

	expMemberInfo := map[string]*memberInfo{
		"phone": &memberInfo{
			gids: map[GroupId]*memberMetaData{
				sgid1: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 10}},
			},
		},
		"tablet": &memberInfo{
			gids: map[GroupId]*memberMetaData{
				sgid1: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 25}},
				sgid2: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 111}},
			},
		},
		"cloud": &memberInfo{
			gids: map[GroupId]*memberMetaData{
				sgid1: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 1}},
			},
		},
		"door": &memberInfo{
			gids: map[GroupId]*memberMetaData{
				sgid2: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 33}},
			},
		},
		"lamp": &memberInfo{
			gids: map[GroupId]*memberMetaData{
				sgid2: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 9}},
			},
		},
	}

	for mm := range members {
		info, err := sg.getMemberInfo(mm)
		if err != nil || info == nil {
			t.Errorf("cannot get info for SyncGroup member %s: info: %v, err: %v", mm, info, err)
		}
		expInfo := expMemberInfo[mm]
		if !reflect.DeepEqual(info, expInfo) {
			t.Errorf("invalid info for SyncGroup member %s: got %v instead of %v", mm, info, expInfo)
		}
	}

	// Delete the 1st SyncGroup.
	err = sg.delSyncGroupByID(sgid1)
	if err != nil {
		t.Errorf("deleting SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid1, sgfile, err)
	}

	checkSGStats(t, "multi-3", 1, 3)

	// Verify SyncGroup membership data.
	members, err = sg.getMembers()
	if err != nil {
		t.Errorf("cannot get all SyncGroup members: %v", err)
	}

	expMembers = map[string]uint32{"tablet": 1, "door": 1, "lamp": 1}
	if !reflect.DeepEqual(members, expMembers) {
		t.Errorf("invalid SyncGroup members: got %v instead of %v", members, expMembers)
	}

	expMemberInfo = map[string]*memberInfo{
		"tablet": &memberInfo{
			gids: map[GroupId]*memberMetaData{
				sgid2: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 111}},
			},
		},
		"door": &memberInfo{
			gids: map[GroupId]*memberMetaData{
				sgid2: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 33}},
			},
		},
		"lamp": &memberInfo{
			gids: map[GroupId]*memberMetaData{
				sgid2: &memberMetaData{metaData: JoinerMetaData{SyncPriority: 9}},
			},
		},
	}

	for mm := range members {
		info, err := sg.getMemberInfo(mm)
		if err != nil || info == nil {
			t.Errorf("cannot get info for SyncGroup member %s: info: %v, err: %v", mm, info, err)
		}
		expInfo := expMemberInfo[mm]
		if !reflect.DeepEqual(info, expInfo) {
			t.Errorf("invalid info for SyncGroup member %s: got %v instead of %v", mm, info, expInfo)
		}
	}

	sg.dump()
	sg.close()
}
