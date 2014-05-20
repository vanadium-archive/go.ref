package vsync

// Tests for sync garbage collection.
import (
	"os"
	"reflect"
	"testing"

	_ "veyron/lib/testutil"
	"veyron2/storage"
)

// TestGCOnlineConsistencyCheck tests the online consistency check in GC.
func TestGCOnlineConsistencyCheck(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-1obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}

	if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
		t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
	}
	// No objects should be marked for GC.
	if s.hdlGC.checkConsistency {
		t.Errorf("onlineConsistencyCheck didn't finish in test %s", testFile)
	}
	if len(s.hdlGC.pruneObjects) > 0 {
		t.Errorf("onlineConsistencyCheck created unexpected objects in test %s, map: %v",
			testFile, s.hdlGC.pruneObjects)
	}
	if strictCheck && len(s.hdlGC.verifyPruneMap) > 0 {
		t.Errorf("onlineConsistencyCheck created unexpected objects in test %s, map: %v",
			testFile, s.hdlGC.verifyPruneMap)
	}

	// Fast-forward the reclaimSnap.
	s.hdlGC.reclaimSnap = GenVector{"A": 2, "B": 1, "C": 2}

	if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
		t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
	}
	// Nothing should change since ock is false.
	if len(s.hdlGC.pruneObjects) > 0 {
		t.Errorf("onlineConsistencyCheck created unexpected objects in test %s, map: %v",
			testFile, s.hdlGC.pruneObjects)
	}
	if strictCheck && len(s.hdlGC.verifyPruneMap) > 0 {
		t.Errorf("onlineConsistencyCheck created unexpected objects in test %s, map: %v",
			testFile, s.hdlGC.verifyPruneMap)
	}
	expVec := GenVector{"A": 2, "B": 1, "C": 2}
	// Ensuring reclaimSnap didn't get modified in onlineConsistencyCheck().
	if !reflect.DeepEqual(expVec, s.hdlGC.reclaimSnap) {
		t.Errorf("Data mismatch for reclaimSnap: %v instead of %v", s.hdlGC.reclaimSnap, expVec)
	}

	s.hdlGC.checkConsistency = true
	genBatchSize = 0
	if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
		t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
	}
	// Nothing should change since genBatchSize is 0.
	if len(s.hdlGC.pruneObjects) > 0 {
		t.Errorf("onlineConsistencyCheck created unexpected objects in test %s, map: %v",
			testFile, s.hdlGC.pruneObjects)
	}
	if strictCheck && len(s.hdlGC.verifyPruneMap) > 0 {
		t.Errorf("onlineConsistencyCheck created unexpected objects in test %s, map: %v",
			testFile, s.hdlGC.verifyPruneMap)
	}
	if !reflect.DeepEqual(expVec, s.hdlGC.reclaimSnap) {
		t.Errorf("Data mismatch for reclaimSnap: %v instead of %v", s.hdlGC.reclaimSnap, expVec)
	}

	// Test batching.
	genBatchSize = 1
	if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
		t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
	}
	if !s.hdlGC.checkConsistency {
		t.Errorf("onlineConsistencyCheck finished in test %s", testFile)
	}
	total := (expVec[DeviceID("A")] - s.hdlGC.reclaimSnap[DeviceID("A")]) +
		(expVec[DeviceID("B")] - s.hdlGC.reclaimSnap[DeviceID("B")]) +
		(expVec[DeviceID("C")] - s.hdlGC.reclaimSnap[DeviceID("C")])
	if total != 1 {
		t.Errorf("onlineConsistencyCheck failed in test %s, %v", testFile, s.hdlGC.reclaimSnap)
	}

	genBatchSize = 4
	if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
		t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
	}

	objid, err := strToObjID("12345")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}
	expMap := make(map[storage.ID]*objGCState)
	expMap[objid] = &objGCState{pos: 4, version: 4}
	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map in vsyncd: %v instead of %v",
			s.hdlGC.pruneObjects[objid], expMap[objid])
	}
	expMap1 := make(map[storage.ID]*objVersHist)
	if strictCheck {
		expMap1[objid] = &objVersHist{versions: make(map[storage.Version]struct{})}
		for i := 0; i < 5; i++ {
			expMap1[objid].versions[storage.Version(i)] = struct{}{}
		}
		if !reflect.DeepEqual(expMap1, s.hdlGC.verifyPruneMap) {
			t.Errorf("Data mismatch for verifyPruneMap: %v instead of %v", s.hdlGC.verifyPruneMap, expMap1)
		}
	}
	expVec = GenVector{"A": 0, "B": 0, "C": 0}
	if !reflect.DeepEqual(expVec, s.hdlGC.reclaimSnap) {
		t.Errorf("Data mismatch for reclaimSnap: %v instead of %v", s.hdlGC.reclaimSnap, expVec)
	}
	if s.hdlGC.checkConsistency {
		t.Errorf("onlineConsistencyCheck didn't finish in test %s", testFile)
	}
}

// TestGCGeneration tests the garbage collection of a generation.
func TestGCGeneration(t *testing.T) {
	// Run the test for both values of strictCheck flag.
	flags := []bool{true, false}
	for _, val := range flags {
		strictCheck = val
		setupGarbageCollectGeneration(t)
	}
}

// setupGarbageCollectGeneration performs the setup to test garbage collection of a generation.
func setupGarbageCollectGeneration(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-1obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}

	// Test GenID of 0.
	if err := s.hdlGC.garbageCollectGeneration(DeviceID("A"), 0); err != nil {
		t.Errorf("garbageCollectGeneration failed for test %s, dev A gnum 0, err %v\n", testFile, err)
	}

	// Test a non-existent generation.
	if err := s.hdlGC.garbageCollectGeneration(DeviceID("A"), 10); err == nil {
		t.Errorf("garbageCollectGeneration failed for test %s, dev A gnum 10\n", testFile)
	}

	if err := s.hdlGC.garbageCollectGeneration(DeviceID("A"), 2); err != nil {
		t.Errorf("garbageCollectGeneration failed for test %s, dev A gnum 2, err %v\n", testFile, err)
	}

	objid, err := strToObjID("12345")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}

	expMap := make(map[storage.ID]*objGCState)
	expMap[objid] = &objGCState{pos: 3, version: 3}
	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map: %v instead of %v", s.hdlGC.pruneObjects, expMap)
	}

	expMap1 := make(map[storage.ID]*objVersHist)
	if strictCheck {
		expMap1[objid] = &objVersHist{versions: make(map[storage.Version]struct{})}
		expMap1[objid].versions[storage.Version(3)] = struct{}{}
		if !reflect.DeepEqual(expMap1, s.hdlGC.verifyPruneMap) {
			t.Errorf("Data mismatch for verifyPruneMap: %v instead of %v", s.hdlGC.verifyPruneMap, expMap1)
		}
	}

	// Test GC'ing a generation lower than A:2.
	if err := s.hdlGC.garbageCollectGeneration(DeviceID("A"), 1); err != nil {
		t.Errorf("garbageCollectGeneration failed for test %s, dev A gnum 1, err %v\n", testFile, err)
	}
	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map: %v instead of %v", s.hdlGC.pruneObjects, expMap)
	}

	if strictCheck {
		expMap1[objid].versions[storage.Version(2)] = struct{}{}
		if !reflect.DeepEqual(expMap1, s.hdlGC.verifyPruneMap) {
			t.Errorf("Data mismatch for verifyPruneMap: %v instead of %v", s.hdlGC.verifyPruneMap, expMap1)
		}
	}

	// Test GC'ing a generation higher than A:2.
	if err := s.hdlGC.garbageCollectGeneration(DeviceID("B"), 3); err != nil {
		t.Errorf("garbageCollectGeneration failed for test %s, dev B gnum 3, err %v\n", testFile, err)
	}
	expMap[objid].pos = 6
	expMap[objid].version = 6
	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map: %v instead of %v", s.hdlGC.pruneObjects, expMap)
	}

	if strictCheck {
		expMap1[objid].versions[storage.Version(6)] = struct{}{}
		if !reflect.DeepEqual(expMap1, s.hdlGC.verifyPruneMap) {
			t.Errorf("Data mismatch for verifyPruneMap: %v instead of %v",
				s.hdlGC.verifyPruneMap[objid], expMap1[objid])
		}
	}
}

// TestGCReclaimSpace tests the space reclamation algorithm in GC.
func TestGCReclaimSpace(t *testing.T) {
	// Run the tests for both values of strictCheck flag.
	flags := []bool{true, false}
	for _, val := range flags {
		strictCheck = val
		setupGCReclaimSpace1Obj(t)
		setupGCReclaimSpace3Objs(t)
	}
}

// setupGCReclaimSpace performs the setup to test space reclamation for a scenario with 1 object.
func setupGCReclaimSpace1Obj(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-1obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}

	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}

	objid, err := strToObjID("12345")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}

	expMap := make(map[storage.ID]*objGCState)
	expMap[objid] = &objGCState{pos: 4, version: 4}
	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map: %v instead of %v", s.hdlGC.pruneObjects[objid], expMap[objid])
	}

	expMap1 := make(map[storage.ID]*objVersHist)
	expMap1[objid] = &objVersHist{versions: make(map[storage.Version]struct{})}
	if strictCheck {
		for i := 0; i < 5; i++ {
			expMap1[objid].versions[storage.Version(i)] = struct{}{}
		}
		if !reflect.DeepEqual(expMap1, s.hdlGC.verifyPruneMap) {
			t.Errorf("Data mismatch for verifyPruneMap: %v instead of %v",
				s.hdlGC.verifyPruneMap[objid], expMap1[objid])
		}
	}
	expVec := GenVector{"A": 2, "B": 1, "C": 2}
	if !reflect.DeepEqual(expVec, s.devtab.head.ReclaimVec) {
		t.Errorf("Data mismatch for reclaimVec: %v instead of %v",
			s.devtab.head.ReclaimVec, expVec)
	}

	// Allow for GCing B:3 incrementally.
	cVec := GenVector{"A": 2, "B": 3, "C": 2}
	if err := s.devtab.putGenVec(DeviceID("C"), cVec); err != nil {
		t.Errorf("putGenVec failed for test %s, err %v\n", testFile, err)
	}
	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}
	expMap[objid] = &objGCState{pos: 6, version: 6}
	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map: %v instead of %v", s.hdlGC.pruneObjects[objid], expMap[objid])
	}
	if strictCheck {
		expMap1[objid].versions[storage.Version(5)] = struct{}{}
		expMap1[objid].versions[storage.Version(6)] = struct{}{}
		if !reflect.DeepEqual(expMap1, s.hdlGC.verifyPruneMap) {
			t.Errorf("Data mismatch for verifyPruneMap: %v instead of %v",
				s.hdlGC.verifyPruneMap[objid], expMap[objid])
		}
	}
	if !reflect.DeepEqual(cVec, s.devtab.head.ReclaimVec) {
		t.Errorf("Data mismatch for reclaimVec: %v instead of %v",
			s.devtab.head.ReclaimVec, cVec)
	}
}

// setupGCReclaimSpace3Objs performs the setup to test space reclamation for a scenario with 3 objects.
func setupGCReclaimSpace3Objs(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-3obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}

	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}

	expMap := make(map[storage.ID]*objGCState)
	expMap1 := make(map[storage.ID]*objVersHist)

	obj1, err := strToObjID("123")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}
	expMap[obj1] = &objGCState{pos: 8, version: 6}
	expMap1[obj1] = &objVersHist{versions: make(map[storage.Version]struct{})}
	for i := 1; i < 7; i++ {
		expMap1[obj1].versions[storage.Version(i)] = struct{}{}
	}

	obj2, err := strToObjID("456")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}
	expMap[obj2] = &objGCState{pos: 10, version: 7}
	expMap1[obj2] = &objVersHist{versions: make(map[storage.Version]struct{})}
	for i := 1; i < 6; i++ {
		expMap1[obj2].versions[storage.Version(i)] = struct{}{}
	}
	expMap1[obj2].versions[storage.Version(7)] = struct{}{}

	obj3, err := strToObjID("789")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}
	expMap[obj3] = &objGCState{pos: 8, version: 4}
	expMap1[obj3] = &objVersHist{versions: make(map[storage.Version]struct{})}
	for i := 1; i < 5; i++ {
		expMap1[obj3].versions[storage.Version(i)] = struct{}{}
	}

	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map: %v instead of %v", s.hdlGC.pruneObjects, expMap)
	}
	if strictCheck {
		if !reflect.DeepEqual(expMap1, s.hdlGC.verifyPruneMap) {
			t.Errorf("Data mismatch for verifyPruneMap: %v instead of %v", s.hdlGC.verifyPruneMap, expMap1)
		}
	}
	expVec := GenVector{"A": 4, "B": 3, "C": 4}
	if !reflect.DeepEqual(expVec, s.devtab.head.ReclaimVec) {
		t.Errorf("Data mismatch for reclaimVec: %v instead of %v",
			s.devtab.head.ReclaimVec, expVec)
	}

	// Advance GC by one more generation.
	expVec[DeviceID("A")] = 5
	expVec[DeviceID("C")] = 5
	if err := s.devtab.putGenVec(DeviceID("C"), expVec); err != nil {
		t.Errorf("putGenVec failed for test %s, err %v\n", testFile, err)
	}
	if err := s.devtab.putGenVec(DeviceID("B"), expVec); err != nil {
		t.Errorf("putGenVec failed for test %s, err %v\n", testFile, err)
	}

	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}

	expMap[obj1] = &objGCState{pos: 12, version: 9}
	for i := 7; i < 10; i++ {
		expMap1[obj1].versions[storage.Version(i)] = struct{}{}
	}
	expMap[obj2] = &objGCState{pos: 12, version: 8}
	for i := 6; i < 9; i++ {
		expMap1[obj2].versions[storage.Version(i)] = struct{}{}
	}
	expMap[obj3] = &objGCState{pos: 12, version: 6}
	for i := 5; i < 7; i++ {
		expMap1[obj3].versions[storage.Version(i)] = struct{}{}
	}

	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map: %v instead of %v", s.hdlGC.pruneObjects, expMap)
	}
	if strictCheck {
		if !reflect.DeepEqual(expMap1, s.hdlGC.verifyPruneMap) {
			t.Errorf("Data mismatch for verifyPruneMap: %v instead of %v", s.hdlGC.verifyPruneMap, expMap1)
		}
	}
	expVec = GenVector{"A": 5, "B": 3, "C": 5}
	if !reflect.DeepEqual(expVec, s.devtab.head.ReclaimVec) {
		t.Errorf("Data mismatch for reclaimVec: %v instead of %v",
			s.devtab.head.ReclaimVec, expVec)
	}
}

// TestGCDAGPruneCallBack tests the callback function called by dag prune.
func TestGCDAGPruneCallBack(t *testing.T) {
	// Run the tests for both values of strictCheck flag.
	flags := []bool{true, false}
	for _, val := range flags {
		strictCheck = val
		setupGCDAGPruneCallBack(t)
		setupGCDAGPruneCallBackStrict(t)
		setupGCDAGPruneCBPartGen(t)
	}
}

// setupGCDAGPruneCallBack performs the setup to test the callback function given to dag prune.
func setupGCDAGPruneCallBack(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	if err := s.hdlGC.dagPruneCallBack("A:1:0"); err == nil {
		t.Errorf("dagPruneCallBack error check failed\n")
	}

	testFile := "test-1obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	s.devtab.head.ReclaimVec = GenVector{"A": 2, "B": 1, "C": 2}

	// Call should succeed irrespective of strictCheck since "ock" is true.
	if strictCheck {
		objid, err := strToObjID("12345")
		if err != nil {
			t.Errorf("Could not create objid %v", err)
		}
		s.hdlGC.verifyPruneMap[objid] = &objVersHist{
			versions: make(map[storage.Version]struct{}),
		}
	}
	if err := s.hdlGC.dagPruneCallBack("A:1:0"); err != nil {
		t.Errorf("dagPruneCallBack failed for test %s, err %v\n", testFile, err)
	}

	// Calling the same key after success should fail.
	if err := s.hdlGC.dagPruneCallBack("A:1:0"); err == nil {
		t.Errorf("dagPruneCallBack failed for test %s, err %v\n", testFile, err)
	}

	if s.log.hasLogRec(DeviceID("A"), GenID(1), LSN(0)) {
		t.Errorf("Log record still exists for test %s\n", testFile)
	}
	if s.log.hasGenMetadata(DeviceID("A"), GenID(1)) {
		t.Errorf("Gen metadata still exists for test %s\n", testFile)
	}
}

// setupGCDAGPruneCallBackStrict performs the setup to test the
// callback function called by dag prune when strictCheck is true.
func setupGCDAGPruneCallBackStrict(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-1obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	s.devtab.head.ReclaimVec = GenVector{"A": 2, "B": 1, "C": 2}
	if !strictCheck {
		return
	}

	s.hdlGC.checkConsistency = false
	if err := s.hdlGC.dagPruneCallBack("A:1:0"); err == nil {
		t.Errorf("dagPruneCallBack should have failed for test %s\n", testFile)
	}

	objid, err := strToObjID("12345")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}
	s.hdlGC.verifyPruneMap[objid] = &objVersHist{
		versions: make(map[storage.Version]struct{}),
	}
	s.hdlGC.verifyPruneMap[objid].versions[storage.Version(2)] = struct{}{}
	if err := s.hdlGC.dagPruneCallBack("A:1:0"); err != nil {
		t.Errorf("dagPruneCallBack failed for test %s, err %v\n", testFile, err)
	}

	// Calling the same key after success should fail.
	if err := s.hdlGC.dagPruneCallBack("A:1:0"); err == nil {
		t.Errorf("dagPruneCallBack should have failed for test %s, err %v\n", testFile, err)
	}
	if s.log.hasLogRec(DeviceID("A"), GenID(1), LSN(0)) {
		t.Errorf("Log record still exists for test %s\n", testFile)
	}
	if s.log.hasGenMetadata(DeviceID("A"), GenID(1)) {
		t.Errorf("Gen metadata still exists for test %s\n", testFile)
	}
	if len(s.hdlGC.verifyPruneMap[objid].versions) > 0 {
		t.Errorf("Unexpected object version in test %s, map: %v",
			testFile, s.hdlGC.verifyPruneMap[objid])
	}
}

// setupGCDAGPruneCBPartGen performs the setup to test the callback
// function called by dag prune when only one entry from a generation
// (partial gen) is pruned.
func setupGCDAGPruneCBPartGen(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-3obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	s.devtab.head.ReclaimVec = GenVector{"A": 4, "B": 3, "C": 4}
	s.hdlGC.checkConsistency = false
	if strictCheck {
		objid, err := strToObjID("789")
		if err != nil {
			t.Errorf("Could not create objid %v", err)
		}
		s.hdlGC.verifyPruneMap[objid] = &objVersHist{
			versions: make(map[storage.Version]struct{}),
		}
		s.hdlGC.verifyPruneMap[objid].versions[storage.Version(4)] = struct{}{}
	}

	// Before pruning.
	expGen := &genMetadata{Pos: 8, Count: 3, MaxLSN: 2}
	gen, err := s.log.getGenMetadata(DeviceID("A"), GenID(3))
	if err != nil {
		t.Errorf("getGenMetadata failed for test %s, err %v\n", testFile, err)
	}
	if !reflect.DeepEqual(expGen, gen) {
		t.Errorf("Data mismatch for genMetadata: %v instead of %v",
			gen, expGen)
	}

	if err := s.hdlGC.dagPruneCallBack("A:3:2"); err != nil {
		t.Errorf("dagPruneCallBack failed for test %s, err %v\n", testFile, err)
	}

	if s.log.hasLogRec(DeviceID("A"), GenID(3), LSN(2)) {
		t.Errorf("Log record still exists for test %s\n", testFile)
	}
	if !s.log.hasGenMetadata(DeviceID("A"), GenID(3)) {
		t.Errorf("Gen metadata still exists for test %s\n", testFile)
	}
	expGen = &genMetadata{Pos: 8, Count: 2, MaxLSN: 2}
	gen, err = s.log.getGenMetadata(DeviceID("A"), GenID(3))
	if err != nil {
		t.Errorf("getGenMetadata failed for test %s, err %v\n", testFile, err)
	}
	if !reflect.DeepEqual(expGen, gen) {
		t.Errorf("Data mismatch for genMetadata: %v instead of %v",
			gen, expGen)
	}

	if strictCheck {
		objid, err := strToObjID("123")
		if err != nil {
			t.Errorf("Could not create objid %v", err)
		}
		s.hdlGC.verifyPruneMap[objid] = &objVersHist{
			versions: make(map[storage.Version]struct{}),
		}
		s.hdlGC.verifyPruneMap[objid].versions[storage.Version(6)] = struct{}{}
	}
	if err := s.hdlGC.dagPruneCallBack("A:3:0"); err != nil {
		t.Errorf("dagPruneCallBack failed for test %s, err %v\n", testFile, err)
	}
	expGen = &genMetadata{Pos: 8, Count: 1, MaxLSN: 2}
	gen, err = s.log.getGenMetadata(DeviceID("A"), GenID(3))
	if err != nil {
		t.Errorf("getGenMetadata failed for test %s, err %v\n", testFile, err)
	}
	if !reflect.DeepEqual(expGen, gen) {
		t.Errorf("Data mismatch for genMetadata: %v instead of %v",
			gen, expGen)
	}
}

// TestGCPruning tests the object pruning in GC.
func TestGCPruning(t *testing.T) {
	// Run the tests for both values of strictCheck flag.
	flags := []bool{true, false}
	for _, val := range flags {
		strictCheck = val
		setupGCPruneObject(t)
		setupGCPruneObjectBatching(t)
		setupGCPrune3Objects(t)
	}
}

// setupGCPruneObject performs the setup to test pruning an object.
func setupGCPruneObject(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-1obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}

	s.hdlGC.checkConsistency = false
	objBatchSize = 0
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}
	if len(s.hdlGC.pruneObjects) != 1 {
		t.Errorf("pruneObjectBatch deleted object in test %s, map: %v", testFile, s.hdlGC.pruneObjects)
	}

	objBatchSize = 1
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}

	if len(s.hdlGC.pruneObjects) > 0 {
		t.Errorf("pruneObjectBatch left unexpected objects in test %s, map: %v", testFile, s.hdlGC.pruneObjects)
	}

	// Generations that should have been deleted.
	expVec := GenVector{"A": 2, "B": 1, "C": 1}
	for dev, gen := range expVec {
		for i := GenID(1); i <= gen; i++ {
			if s.log.hasGenMetadata(dev, i) {
				t.Errorf("pruneObjectBatch left unexpected generation in test %s, %s %d",
					testFile, dev, i)
			}
			if s.log.hasLogRec(dev, i, 0) {
				t.Errorf("pruneObjectBatch left unexpected logrec in test %s, %s %d 0",
					testFile, dev, i)
			}
		}
	}

	// Generations that should remain.
	devArr := []DeviceID{"B", "B", "C"}
	genArr := []GenID{2, 3, 2}
	for pos, dev := range devArr {
		if _, err := s.log.getGenMetadata(dev, genArr[pos]); err != nil {
			t.Errorf("pruneObjectBatch didn't find expected generation in test %s, %s %d",
				testFile, dev, genArr[pos])
		}
		if _, err := s.log.getLogRec(dev, genArr[pos], 0); err != nil {
			t.Errorf("pruneObjectBatch didn't find expected logrec in test %s, %s %d 0",
				testFile, dev, genArr[pos])
		}
	}

	// Verify DAG state.
	objid, err := strToObjID("12345")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}
	if head, err := s.dag.getHead(objid); err != nil || head != 6 {
		t.Errorf("Invalid object %d head in DAG %s, err %v", objid, head, err)
	}
}

// setupGCPruneObjectBatching performs the setup to test batching while pruning objects.
func setupGCPruneObjectBatching(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-3obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}

	s.hdlGC.checkConsistency = false
	objBatchSize = 1
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}
	if len(s.hdlGC.pruneObjects) != 2 {
		t.Errorf("pruneObjectBatch didn't remove expected objects in test %s, map: %v", testFile, s.hdlGC.pruneObjects)
	}

	// Add a spurious version to the version history and verify error under strictCheck.
	if strictCheck {
		for _, obj := range s.hdlGC.verifyPruneMap {
			obj.versions[80] = struct{}{}
		}
		if err := s.hdlGC.pruneObjectBatch(); err == nil {
			t.Errorf("pruneObjectBatch didn't fail for test %s, err %v\n", testFile, err)
		}
	}
}

// TestGCPruneObjCheckError checks the error path in pruneObjectBatch under strictCheck.
func TestGCPruneObjCheckError(t *testing.T) {
	if !strictCheck {
		return
	}

	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-3obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}

	s.hdlGC.checkConsistency = false
	objBatchSize = 1

	// Remove the prune point, add a spurious version.
	for id, obj := range s.hdlGC.verifyPruneMap {
		v := s.hdlGC.pruneObjects[id].version
		obj.versions[80] = struct{}{}
		delete(obj.versions, v)
	}

	if err = s.hdlGC.pruneObjectBatch(); err == nil {
		t.Errorf("pruneObjectBatch didn't fail for test %s, err %v\n", testFile, err)
	}
}

// setupGCPrune3Objects performs the setup to test pruning in a 3 object scenario.
func setupGCPrune3Objects(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-3obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}

	objBatchSize = 5
	s.hdlGC.checkConsistency = false
	gen, err := s.log.getGenMetadata("A", 3)
	if err != nil {
		t.Errorf("getGenMetadata failed for test %s, err %v\n", testFile, err)
	}
	if gen.Count != 3 {
		t.Errorf("GenMetadata has incorrect value for test %s\n", testFile)
	}
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}
	if len(s.hdlGC.pruneObjects) > 0 {
		t.Errorf("pruneObjectBatch didn't remove expected objects in test %s, map: %v", testFile, s.hdlGC.pruneObjects)
	}
	// Generations that should have been deleted.
	expVec := GenVector{"A": 2, "B": 3, "C": 4}
	for dev, gnum := range expVec {
		for i := GenID(1); i <= gnum; i++ {
			if s.log.hasGenMetadata(dev, i) {
				t.Errorf("pruneObjectBatch left unexpected generation in test %s, %s %d",
					testFile, dev, i)
			}
			// Check the first log record.
			if s.log.hasLogRec(dev, i, 0) {
				t.Errorf("pruneObjectBatch left unexpected logrec in test %s, %s %d 0",
					testFile, dev, i)
			}
		}
	}
	// Check the partial generation.
	if gen, err = s.log.getGenMetadata("A", 3); err != nil {
		t.Errorf("getGenMetadata failed for test %s, err %v\n", testFile, err)
	}
	if gen.Count != 2 {
		t.Errorf("GenMetadata has incorrect value for test %s\n", testFile)
	}
	// Verify DAG state.
	objArr := []string{"123", "456", "789"}
	heads := []storage.Version{10, 8, 6}
	for pos, o := range objArr {
		objid, err := strToObjID(o)
		if err != nil {
			t.Errorf("Could not create objid %v", err)
		}
		if head, err := s.dag.getHead(objid); err != nil || head != heads[pos] {
			t.Errorf("Invalid object %d head in DAG %s, err %v", objid, head, err)
		}
	}
}

// TestGCStages tests the interactions across the different stages in GC.
func TestGCStages(t *testing.T) {
	// Run the tests for both values of strictCheck flag.
	flags := []bool{true, false}
	for _, val := range flags {
		strictCheck = val
		setupGCReclaimAndOnlineCk(t)
		setupGCReclaimAndOnlineCkIncr(t)
		setupGCOnlineCkAndPrune(t)
	}
}

// setupGCReclaimAndOnlineCk performs the setup to test interaction between reclaimSpace and consistency check.
func setupGCReclaimAndOnlineCk(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-3obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}
	objBatchSize = 1
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}

	// Clean up state to simulate a reboot. Given that 1 object
	// is already GC'ed, there are now partial generations left in
	// the state.
	for obj, _ := range s.hdlGC.pruneObjects {
		delete(s.hdlGC.pruneObjects, obj)
		delete(s.hdlGC.verifyPruneMap, obj)
	}
	s.hdlGC.reclaimSnap = GenVector{"A": 4, "B": 3, "C": 4}
	genBatchSize = 1
	for s.hdlGC.checkConsistency == true {
		if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
			t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
		}
	}

	// Since dag prunes everything older than a version, all 3
	// objects show up once again in the pruneObjects map.
	if len(s.hdlGC.pruneObjects) != 3 {
		t.Errorf("onlineConsistencyCheck didn't add objects in test %s, map: %v", testFile, s.hdlGC.pruneObjects)
	}

	objBatchSize = 3
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}
	if len(s.hdlGC.pruneObjects) > 0 {
		t.Errorf("pruneObjectBatch didn't remove expected objects in test %s, map: %v", testFile, s.hdlGC.pruneObjects)
	}
	// Generations that should have been deleted.
	expVec := GenVector{"A": 2, "B": 3, "C": 4}
	for dev, gnum := range expVec {
		for i := GenID(1); i <= gnum; i++ {
			if s.log.hasGenMetadata(dev, i) {
				t.Errorf("pruneObjectBatch left unexpected generation in test %s, %s %d",
					testFile, dev, i)
			}
			// Check the first log record.
			if s.log.hasLogRec(dev, i, 0) {
				t.Errorf("pruneObjectBatch left unexpected logrec in test %s, %s %d 0",
					testFile, dev, i)
			}
		}
	}
	// Check the partial generation.
	gen, err := s.log.getGenMetadata("A", 3)
	if err != nil {
		t.Errorf("getGenMetadata failed for test %s, err %v\n", testFile, err)
	}
	if gen.Count != 2 {
		t.Errorf("GenMetadata has incorrect value for test %s\n", testFile)
	}
	// Verify DAG state.
	objArr := []string{"123", "456", "789"}
	heads := []storage.Version{10, 8, 6}
	for pos, o := range objArr {
		objid, err := strToObjID(o)
		if err != nil {
			t.Errorf("Could not create objid %v", err)
		}
		if head, err := s.dag.getHead(objid); err != nil || head != heads[pos] {
			t.Errorf("Invalid object %d head in DAG %s, err %v", objid, head, err)
		}
	}
}

// setupGCReclaimAndOnlineCkIncr tests interaction between reclaimSpace
// and consistency check when both are running one after the other
// incrementally.
func setupGCReclaimAndOnlineCkIncr(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-1obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	// Fast-forward the reclaimSnap and ReclaimVec.
	s.hdlGC.reclaimSnap = GenVector{"A": 2, "B": 1, "C": 2}
	s.devtab.head.ReclaimVec = GenVector{"A": 2, "B": 1, "C": 2}

	genBatchSize = 1
	if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
		t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
	}
	objBatchSize = 1
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}

	cVec := GenVector{"A": 2, "B": 3, "C": 2}
	if err := s.devtab.putGenVec(DeviceID("C"), cVec); err != nil {
		t.Errorf("putGenVec failed for test %s, err %v\n", testFile, err)
	}
	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}
	objid, err := strToObjID("12345")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}
	expMap := make(map[storage.ID]*objGCState)
	expMap[objid] = &objGCState{pos: 6, version: 6}
	if !reflect.DeepEqual(expMap, s.hdlGC.pruneObjects) {
		t.Errorf("Data mismatch for pruneObjects map: %v instead of %v", s.hdlGC.pruneObjects[objid], expMap[objid])
	}
	objBatchSize = 1
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}
	for s.hdlGC.checkConsistency == true {
		if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
			t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
		}
	}
	if len(s.hdlGC.pruneObjects) > 0 {
		t.Errorf("onlineConsistencyCheck created an object in test %s, map: %v", testFile, s.hdlGC.pruneObjects)
	}
}

// setupGCOnlineCkAndPrune performs the setup to test interaction
// between consistency check and object pruning (when both are
// incremental).
func setupGCOnlineCkAndPrune(t *testing.T) {
	dir, err := createTempDir()
	if err != nil {
		t.Errorf("Could not create tempdir %v", err)
	}
	s := NewSyncd("", "", "A", dir, "", 0)

	defer s.Close()
	defer os.RemoveAll(dir)

	testFile := "test-3obj.gc.sync"
	if err := vsyncInitState(s, testFile); err != nil {
		t.Error(err)
	}
	if err := s.hdlGC.reclaimSpace(); err != nil {
		t.Errorf("reclaimSpace failed for test %s, err %v\n", testFile, err)
	}
	objBatchSize = 1
	if err := s.hdlGC.pruneObjectBatch(); err != nil {
		t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
	}

	// Clean up state to simulate a reboot. Given that 1 object
	// is already GC'ed, there are now partial generations left in
	// the state.
	for obj, _ := range s.hdlGC.pruneObjects {
		delete(s.hdlGC.pruneObjects, obj)
		delete(s.hdlGC.verifyPruneMap, obj)
	}
	s.hdlGC.reclaimSnap = GenVector{"A": 4, "B": 3, "C": 4}
	genBatchSize = 3
	objBatchSize = 3
	for s.hdlGC.checkConsistency == true {
		if err := s.hdlGC.onlineConsistencyCheck(); err != nil {
			t.Fatalf("onlineConsistencyCheck failed for test %s, err %v", testFile, err)
		}
		if err := s.hdlGC.pruneObjectBatch(); err != nil {
			t.Errorf("pruneObjectBatch failed for test %s, err %v\n", testFile, err)
		}
	}
	if len(s.hdlGC.pruneObjects) > 0 {
		t.Errorf("pruneObjectBatch didn't remove expected objects in test %s, map: %v", testFile, s.hdlGC.pruneObjects)
	}
}
