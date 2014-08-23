package vsync

// Tests for the Veyron SyncGroup Table.

import (
	"os"
	"reflect"
	"testing"

	"veyron/services/syncgroup"
)

// strToSGID converts a string to a SyncGroup ID.
// This is a temporary function until SyncGroupServer provides an equivalent function.
func strToSGID(s string) (syncgroup.ID, error) {
	var sgid syncgroup.ID
	oid, err := strToObjID(s)
	if err == nil {
		sgid = syncgroup.ID(oid)
	}
	return sgid, err
}

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
		t.Fatalf("SyncGroup Table file %s not created", sgfile)
	}

	sg.flush()
	oldfsize := fsize
	fsize = getFileSize(sgfile)
	if fsize <= oldfsize {
		t.Fatalf("SyncGroup Table file %s not flushed", sgfile)
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
		t.Fatalf("openSyncGroupTable() did not fail when using a bad pathname")
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

	sgid, err := strToSGID("1234")
	if err != nil {
		t.Error(err)
	}

	validateError := func(t *testing.T, err error, funcName string) {
		if err == nil || err.Error() != "invalid SyncGroup Table" {
			t.Errorf("%s() did not fail on a closed SyncGroup Table: %v", funcName, err)
		}
	}

	err = sg.compact()
	validateError(t, err, "compact")

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
	sg.flush()
	sg.close()
	sg.addMember("foobar", sgid, "foobar identity", syncgroup.JoinerMetaData{})
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

// TestAddSyncGroup tests adding SyncGroups.
func TestAddSyncGroup(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	sgname := "foobar"
	sgid, err := strToSGID("1234")
	if err != nil {
		t.Fatal(err)
	}
	rootid, err := strToObjID("5678")
	if err != nil {
		t.Fatal(err)
	}

	sgData := &syncGroupData{
		SrvInfo: syncgroup.SyncGroupInfo{
			SGOID:   sgid,
			RootOID: rootid,
			Name:    sgname,
			Joiners: map[syncgroup.NameIdentity]syncgroup.JoinerMetaData{
				syncgroup.NameIdentity{Name: "phone", Identity: "A"}:  syncgroup.JoinerMetaData{SyncPriority: 10},
				syncgroup.NameIdentity{Name: "tablet", Identity: "B"}: syncgroup.JoinerMetaData{SyncPriority: 25},
				syncgroup.NameIdentity{Name: "cloud", Identity: "C"}:  syncgroup.JoinerMetaData{SyncPriority: 1},
			},
		},
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
		"phone":  &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 10}, identity: "A"},
		"tablet": &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 25}, identity: "B"},
		"cloud":  &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 1}, identity: "C"},
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

	// Use a non-existent member.
	if info, err := sg.getMemberInfo("should-not-be-there"); err == nil {
		t.Errorf("found info for invalid SyncGroup member: %v", info)
	}

	// Adding a SyncGroup for a pre-existing group ID or name should fail.
	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("re-adding SyncGroup %d did not fail", sgid)
	}

	sgData.SrvInfo.SGOID, err = strToSGID("5555")
	if err != nil {
		t.Fatal(err)
	}
	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("adding SyncGroup %s with a different ID did not fail", sgname)
	}

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
	sgid, err := strToSGID("1234")
	if err != nil {
		t.Fatal(err)
	}

	err = sg.addSyncGroup(nil)
	if err == nil {
		t.Errorf("adding a nil SyncGroup did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData := &syncGroupData{}
	sgData.SrvInfo.SGOID = sgid
	sgData.SrvInfo.RootOID, err = strToObjID("5678")
	if err != nil {
		t.Fatal(err)
	}

	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("adding a SyncGroup with an empty name did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData.SrvInfo.Name = sgname

	err = sg.addSyncGroup(sgData)
	if err == nil {
		t.Errorf("adding a SyncGroup with no joiners did not fail in SyncGroup Table file %s", sgfile)
	}

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

	sgData.SrvInfo.Name = "blabla"
	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with no joiners did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData.SrvInfo.Joiners = map[syncgroup.NameIdentity]syncgroup.JoinerMetaData{
		syncgroup.NameIdentity{Name: "phone", Identity: "X"}: syncgroup.JoinerMetaData{SyncPriority: 10},
	}
	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with a non-existing name did not fail in SyncGroup Table file %s", sgfile)
	}

	// Create the SyncGroup to update later.
	sgname := "foobar"
	sgid, err := strToSGID("1234")
	if err != nil {
		t.Fatal(err)
	}
	rootid, err := strToObjID("5678")
	if err != nil {
		t.Fatal(err)
	}

	sgData = &syncGroupData{
		SrvInfo: syncgroup.SyncGroupInfo{
			SGOID:   sgid,
			RootOID: rootid,
			Name:    sgname,
			Joiners: map[syncgroup.NameIdentity]syncgroup.JoinerMetaData{
				syncgroup.NameIdentity{Name: "phone", Identity: "A"}:  syncgroup.JoinerMetaData{SyncPriority: 10},
				syncgroup.NameIdentity{Name: "tablet", Identity: "B"}: syncgroup.JoinerMetaData{SyncPriority: 25},
				syncgroup.NameIdentity{Name: "cloud", Identity: "C"}:  syncgroup.JoinerMetaData{SyncPriority: 1},
			},
		},
	}

	err = sg.addSyncGroup(sgData)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	// Update it using different group or root IDs, which is not allowed.
	xid, err := strToSGID("9999")
	if err != nil {
		t.Fatal(err)
	}

	sgData.SrvInfo.SGOID = xid

	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with an ID mismatch did not fail in SyncGroup Table file %s", sgfile)
	}

	sgData.SrvInfo.SGOID = sgid
	sgData.SrvInfo.RootOID, err = strToObjID("9999")
	if err != nil {
		t.Fatal(err)
	}

	err = sg.updateSyncGroup(sgData)
	if err == nil {
		t.Errorf("updating a SyncGroup with a root ID mismatch did not fail in SyncGroup Table file %s", sgfile)
	}

	// Update it using a modified set of joiners.
	sgData.SrvInfo.RootOID = rootid
	sgData.SrvInfo.Joiners[syncgroup.NameIdentity{Name: "universe", Identity: "Y"}] = syncgroup.JoinerMetaData{SyncPriority: 0}
	delete(sgData.SrvInfo.Joiners, syncgroup.NameIdentity{Name: "cloud", Identity: "C"})

	err = sg.updateSyncGroup(sgData)
	if err != nil {
		t.Errorf("updating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	// Do some NOP member deletions (bad member, bad group ID).
	// SyncGroup verification (below) should see the expected info asserting these were NOPs.
	sg.delMember("blablablablabla", sgid)
	sg.delMember("phone", xid)

	// Verify updated SyncGroup.
	if id, err := sg.getSyncGroupID(sgname); err != nil || id != sgid {
		t.Errorf("cannot get back ID of updated SyncGroup %s: got ID %d instead of %d; err: %v", sgname, id, sgid, err)
	}
	if name, err := sg.getSyncGroupName(sgid); err != nil || name != sgname {
		t.Errorf("cannot get back name of updated SyncGroup ID %d: got %s instead of %s; err: %v", sgid, name, sgname, err)
	}

	expData := &syncGroupData{
		SrvInfo: syncgroup.SyncGroupInfo{
			SGOID:   sgid,
			RootOID: rootid,
			Name:    sgname,
			Joiners: map[syncgroup.NameIdentity]syncgroup.JoinerMetaData{
				syncgroup.NameIdentity{Name: "phone", Identity: "A"}:    syncgroup.JoinerMetaData{SyncPriority: 10},
				syncgroup.NameIdentity{Name: "tablet", Identity: "B"}:   syncgroup.JoinerMetaData{SyncPriority: 25},
				syncgroup.NameIdentity{Name: "universe", Identity: "Y"}: syncgroup.JoinerMetaData{SyncPriority: 0},
			},
		},
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
		"phone":    &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 10}, identity: "A"},
		"tablet":   &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 25}, identity: "B"},
		"universe": &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 0}, identity: "Y"},
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
	sgid, err := strToSGID("1234")
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

	// Create the SyncGroup to delete later.
	rootid, err := strToObjID("5678")
	if err != nil {
		t.Fatal(err)
	}

	sgData := &syncGroupData{
		SrvInfo: syncgroup.SyncGroupInfo{
			SGOID:   sgid,
			RootOID: rootid,
			Name:    sgname,
			Joiners: map[syncgroup.NameIdentity]syncgroup.JoinerMetaData{
				syncgroup.NameIdentity{Name: "phone", Identity: "A"}:  syncgroup.JoinerMetaData{SyncPriority: 10},
				syncgroup.NameIdentity{Name: "tablet", Identity: "B"}: syncgroup.JoinerMetaData{SyncPriority: 25},
				syncgroup.NameIdentity{Name: "cloud", Identity: "C"}:  syncgroup.JoinerMetaData{SyncPriority: 1},
			},
		},
	}

	err = sg.addSyncGroup(sgData)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	// Delete it by ID.
	err = sg.delSyncGroupByID(sgid)
	if err != nil {
		t.Errorf("deleting SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	// Create it again then delete it by name.
	err = sg.addSyncGroup(sgData)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	err = sg.delSyncGroupByName(sgname)
	if err != nil {
		t.Errorf("deleting SyncGroup name %s failed in SyncGroup Table file %s: %v", sgname, sgfile, err)
	}

	sg.close()
}

// TestSyncGroupTableCompact tests compacting the SyncGroup Table K/V DB file.
func TestSyncGroupTableCompact(t *testing.T) {
	sgfile := getFileName()
	defer os.Remove(sgfile)

	sg, err := openSyncGroupTable(sgfile)
	if err != nil {
		t.Fatalf("cannot open new SyncGroup Table file %s", sgfile)
	}

	sgname := "foobar"
	sgid, err := strToSGID("1234")
	if err != nil {
		t.Fatal(err)
	}
	rootid, err := strToObjID("5678")
	if err != nil {
		t.Fatal(err)
	}

	// Add a SyncGroup and use flushes to increase the K/V DB file size.
	sgData := &syncGroupData{
		SrvInfo: syncgroup.SyncGroupInfo{
			SGOID:   sgid,
			RootOID: rootid,
			Name:    sgname,
			Joiners: map[syncgroup.NameIdentity]syncgroup.JoinerMetaData{
				syncgroup.NameIdentity{Name: "phone", Identity: "A"}:  syncgroup.JoinerMetaData{SyncPriority: 10},
				syncgroup.NameIdentity{Name: "tablet", Identity: "B"}: syncgroup.JoinerMetaData{SyncPriority: 25},
				syncgroup.NameIdentity{Name: "cloud", Identity: "C"}:  syncgroup.JoinerMetaData{SyncPriority: 1},
			},
		},
	}

	sg.flush()

	err = sg.addSyncGroup(sgData)
	if err != nil {
		t.Errorf("creating SyncGroup ID %d failed in SyncGroup Table file %s: %v", sgid, sgfile, err)
	}

	sg.flush()

	// Verify SyncGroup and membership info after 2 steps: close/reopen, and compact.

	for i := 0; i < 2; i++ {
		switch i {
		case 0:
			sg.close()
			sg, err = openSyncGroupTable(sgfile)
			if err != nil {
				t.Fatalf("cannot re-open SyncGroup Table file %s", sgfile)
			}

		case 1:
			if err = sg.compact(); err != nil {
				t.Fatalf("cannot compact SyncGroup Table file %s", sgfile)
			}
		}

		// Verify SyncGroup data.
		data, err := sg.getSyncGroupByID(sgid)
		if err != nil {
			t.Errorf("cannot get SyncGroup ID %d (iter %d) in SyncGroup Table file %s: %v", sgid, i, sgfile, err)
		}
		if !reflect.DeepEqual(data, sgData) {
			t.Errorf("invalid SyncGroup data for ID %d (iter %d): got %v instead of %v", sgid, i, data, sgData)
		}

		data, err = sg.getSyncGroupByName(sgname)
		if err != nil {
			t.Errorf("cannot get SyncGroup name %s (iter %d) in SyncGroup Table file %s: %v", sgname, i, sgfile, err)
		}
		if !reflect.DeepEqual(data, sgData) {
			t.Errorf("invalid SyncGroup data for name %s (iter %d): got %v instead of %v", sgname, i, data, sgData)
		}

		// Verify membership data.
		members, err := sg.getMembers()
		if err != nil {
			t.Errorf("cannot get all SyncGroup members (iter %d): %v", i, err)
		}
		expMembers := map[string]uint32{"phone": 1, "tablet": 1, "cloud": 1}
		if !reflect.DeepEqual(members, expMembers) {
			t.Errorf("invalid SyncGroup members (iter %d): got %v instead of %v", i, members, expMembers)
		}

		expMetaData := map[string]*memberMetaData{
			"phone":  &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 10}, identity: "A"},
			"tablet": &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 25}, identity: "B"},
			"cloud":  &memberMetaData{metaData: syncgroup.JoinerMetaData{SyncPriority: 1}, identity: "C"},
		}
		for mm := range members {
			info, err := sg.getMemberInfo(mm)
			if err != nil || info == nil {
				t.Errorf("cannot get info for SyncGroup member %s (iter %d): info: %v, err: %v", mm, i, info, err)
			}
			if len(info.gids) != 1 {
				t.Errorf("invalid info for SyncGroup member %s (iter %d): %v", mm, i, info)
			}
			expJoinerMetaData := expMetaData[mm]
			joinerMetaData := info.gids[sgid]
			if !reflect.DeepEqual(joinerMetaData, expJoinerMetaData) {
				t.Errorf("invalid joiner Data for SyncGroup member %s (iter %d) in group ID %d: %v instead of %v",
					mm, i, sgid, joinerMetaData, expJoinerMetaData)
			}
		}
	}

	sg.close()
}
