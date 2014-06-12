package vsync

// Tests for the Veyron Sync DAG component.

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"testing"
	"time"

	"veyron2/storage"
)

// dagFilename generates a filename for a temporary (per unit test) DAG file.
// Do not replace this function with TempFile because TempFile creates the new
// file and the tests must verify that the DAG can create a non-existing file.
func dagFilename() string {
	return fmt.Sprintf("%s/sync_dag_test_%d_%d", os.TempDir(), os.Getpid(), time.Now().UnixNano())
}

// fileSize returns the size of a file.
func fileSize(fname string) int64 {
	finfo, err := os.Stat(fname)
	if err != nil {
		return -1
	}
	return finfo.Size()
}

// TestDAGOpen tests the creation of a DAG, closing and re-opening it.  It also
// verifies that its backing file is created and that a 2nd close is safe.
func TestDAGOpen(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	fsize := fileSize(dagfile)
	if fsize < 0 {
		t.Fatalf("DAG file %s not created", dagfile)
	}

	dag.flush()
	oldfsize := fsize
	fsize = fileSize(dagfile)
	if fsize <= oldfsize {
		t.Fatalf("DAG file %s not flushed", dagfile)
	}

	dag.close()

	dag, err = openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot re-open existing DAG file %s", dagfile)
	}

	oldfsize = fsize
	fsize = fileSize(dagfile)
	if fsize != oldfsize {
		t.Fatalf("DAG file %s size changed across re-open", dagfile)
	}

	dag.close()
	dag.close() // multiple closes should be a safe NOP

	fsize = fileSize(dagfile)
	if fsize != oldfsize {
		t.Fatalf("DAG file %s size changed across close", dagfile)
	}

	// Fail opening a DAG in a non-existent directory.
	_, err = openDAG("/not/really/there/junk.dag")
	if err == nil {
		t.Fatalf("openDAG() did not fail when using a bad pathname")
	}
}

// TestInvalidDAG tests using DAG methods on an invalid (closed) DAG.
func TestInvalidDAG(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	dag.close()

	oid, err := strToObjID("6789")
	if err != nil {
		t.Error(err)
	}

	err = dag.addNode(oid, 4, false, []storage.Version{2, 3}, "foobar")
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("addNode() did not fail on a closed DAG: %v", err)
	}

	err = dag.moveHead(oid, 4)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("moveHead() did not fail on a closed DAG: %v", err)
	}

	_, _, _, _, err = dag.hasConflict(oid)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("hasConflict() did not fail on a closed DAG: %v", err)
	}

	_, err = dag.getLogrec(oid, 4)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("getLogrec() did not fail on a closed DAG: %v", err)
	}

	err = dag.prune(oid, 4, func(lr string) error {
		return nil
	})
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("prune() did not fail on a closed DAG: %v", err)
	}

	node := &dagNode{Level: 15, Parents: []storage.Version{444, 555}, Logrec: "logrec-23"}
	err = dag.setNode(oid, 4, node)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("setNode() did not fail on a closed DAG: %v", err)
	}

	_, err = dag.getNode(oid, 4)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("getNode() did not fail on a closed DAG: %v", err)
	}

	err = dag.delNode(oid, 4)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("delNode() did not fail on a closed DAG: %v", err)
	}

	err = dag.addParent(oid, 4, 2, true)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("addParent() did not fail on a closed DAG: %v", err)
	}

	err = dag.setHead(oid, 4)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("setHead() did not fail on a closed DAG: %v", err)
	}

	_, err = dag.getHead(oid)
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("getHead() did not fail on a closed DAG: %v", err)
	}

	err = dag.compact()
	if err == nil || err.Error() != "invalid DAG" {
		t.Errorf("compact() did not fail on a closed DAG: %v", err)
	}

	// These calls should be harmless NOPs.
	dag.clearGraft()
	dag.flush()
	dag.close()
	if dag.hasNode(oid, 4) {
		t.Errorf("hasNode() found an object on a closed DAG")
	}
	if pmap := dag.getParentMap(oid); len(pmap) != 0 {
		t.Errorf("getParentMap() found data on a closed DAG: %v", pmap)
	}
	if hmap, gmap := dag.getGraftNodes(oid); hmap != nil || gmap != nil {
		t.Errorf("getGraftNodes() found data on a closed DAG: head map: %v, graft map: %v", hmap, gmap)
	}
}

// TestSetNode tests setting and getting a DAG node across DAG open/close/reopen.
func TestSetNode(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	version := storage.Version(0)
	oid, err := strToObjID("111")
	if err != nil {
		t.Fatal(err)
	}

	node, err := dag.getNode(oid, version)
	if err == nil || node != nil {
		t.Errorf("Found non-existent object %d:%d in DAG file %s: %v", oid, version, dagfile, node)
	}

	if dag.hasNode(oid, version) {
		t.Errorf("hasNode() found non-existent object %d:%d in DAG file %s", oid, version, dagfile)
	}

	if logrec, err := dag.getLogrec(oid, version); err == nil || logrec != "" {
		t.Errorf("Non-existent object %d:%d has a logrec in DAG file %s: %v", oid, version, dagfile, logrec)
	}

	node = &dagNode{Level: 15, Parents: []storage.Version{444, 555}, Logrec: "logrec-23"}
	if err = dag.setNode(oid, version, node); err != nil {
		t.Fatalf("Cannot set object %d:%d (%v) in DAG file %s", oid, version, node, dagfile)
	}

	for i := 0; i < 2; i++ {
		node2, err := dag.getNode(oid, version)
		if err != nil || node2 == nil {
			t.Errorf("Cannot find stored object %d:%d (i=%d) in DAG file %s", oid, version, i, dagfile)
		}

		if !dag.hasNode(oid, version) {
			t.Errorf("hasNode() did not find object %d:%d (i=%d) in DAG file %s", oid, version, i, dagfile)
		}

		if !reflect.DeepEqual(node, node2) {
			t.Errorf("Object %d:%d has wrong data (i=%d) in DAG file %s: %v instead of %v",
				oid, version, i, dagfile, node2, node)
		}

		if logrec, err := dag.getLogrec(oid, version); err != nil || logrec != "logrec-23" {
			t.Errorf("Object %d:%d has wrong logrec (i=%d) in DAG file %s: %v",
				oid, version, i, dagfile, logrec)
		}

		if i == 0 {
			dag.flush()
			dag.close()
			dag, err = openDAG(dagfile)
			if err != nil {
				t.Fatalf("Cannot re-open DAG file %s", dagfile)
			}
		}
	}

	dag.close()
}

// TestDelNode tests deleting a DAG node across DAG open/close/reopen.
func TestDelNode(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	version := storage.Version(1)
	oid, err := strToObjID("222")
	if err != nil {
		t.Fatal(err)
	}

	node := &dagNode{Level: 123, Parents: []storage.Version{333}, Logrec: "logrec-789"}
	if err = dag.setNode(oid, version, node); err != nil {
		t.Fatalf("Cannot set object %d:%d (%v) in DAG file %s", oid, version, node, dagfile)
	}

	dag.flush()

	err = dag.delNode(oid, version)
	if err != nil {
		t.Fatalf("Cannot delete object %d:%d in DAG file %s", oid, version, dagfile)
	}

	dag.flush()

	for i := 0; i < 2; i++ {
		node2, err := dag.getNode(oid, version)
		if err == nil || node2 != nil {
			t.Errorf("Found deleted object %d:%d (%v) (i=%d) in DAG file %s", oid, version, node2, i, dagfile)
		}

		if dag.hasNode(oid, version) {
			t.Errorf("hasNode() found deleted object %d:%d (i=%d) in DAG file %s", oid, version, i, dagfile)
		}

		if logrec, err := dag.getLogrec(oid, version); err == nil || logrec != "" {
			t.Errorf("Deleted object %d:%d (i=%d) has logrec in DAG file %s: %v", oid, version, i, dagfile, logrec)
		}

		if i == 0 {
			dag.close()
			dag, err = openDAG(dagfile)
			if err != nil {
				t.Fatalf("Cannot re-open DAG file %s", dagfile)
			}
		}
	}

	dag.close()
}

// TestAddParent tests adding parents to a DAG node.
func TestAddParent(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	version := storage.Version(7)
	oid, err := strToObjID("789")
	if err != nil {
		t.Fatal(err)
	}

	if err = dag.addParent(oid, version, 1, true); err == nil {
		t.Errorf("addParent() did not fail for an unknown object %d:%d in DAG file %s", oid, version, dagfile)
	}

	node := &dagNode{Level: 15, Logrec: "logrec-22"}
	if err = dag.setNode(oid, version, node); err != nil {
		t.Fatalf("Cannot set object %d:%d (%v) in DAG file %s", oid, version, node, dagfile)
	}

	for _, parent := range []storage.Version{1, 2, 3} {
		if err = dag.addParent(oid, version, parent, true); err == nil {
			t.Errorf("addParent() did not reject invalid parent %d for object %d:%d in DAG file %s",
				parent, oid, version, dagfile)
		}

		pnode := &dagNode{Level: 11, Logrec: fmt.Sprint("logrec-%d", parent)}
		if err = dag.setNode(oid, parent, pnode); err != nil {
			t.Fatalf("Cannot set parent object %d:%d (%v) in DAG file %s", oid, parent, pnode, dagfile)
		}

		remote := parent%2 == 0
		for i := 0; i < 2; i++ {
			if err = dag.addParent(oid, version, parent, remote); err != nil {
				t.Errorf("addParent() failed on parent %d, remote %d (i=%d) for object %d:%d in DAG file %s: %v",
					parent, remote, i, oid, version, dagfile, err)
			}
		}
	}

	node2, err := dag.getNode(oid, version)
	if err != nil || node2 == nil {
		t.Errorf("Cannot find stored object %d:%d in DAG file %s", oid, version, dagfile)
	}

	expParents := []storage.Version{1, 2, 3}
	if !reflect.DeepEqual(node2.Parents, expParents) {
		t.Errorf("invalid parents for object %d:%d in DAG file %s: %v instead of %v",
			oid, version, dagfile, node2.Parents, expParents)
	}

	dag.close()
}

// TestSetHead tests setting and getting a DAG head node across DAG open/close/reopen.
func TestSetHead(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	oid, err := strToObjID("333")
	if err != nil {
		t.Fatal(err)
	}

	version, err := dag.getHead(oid)
	if err == nil {
		t.Errorf("Found non-existent object head %d in DAG file %s: %d", oid, dagfile, version)
	}

	version = 555
	if err = dag.setHead(oid, version); err != nil {
		t.Fatalf("Cannot set object head %d (%d) in DAG file %s", oid, version, dagfile)
	}

	dag.flush()

	for i := 0; i < 3; i++ {
		version2, err := dag.getHead(oid)
		if err != nil {
			t.Errorf("Cannot find stored object head %d (i=%d) in DAG file %s", oid, i, dagfile)
		}
		if version != version2 {
			t.Errorf("Object %d has wrong head data (i=%d) in DAG file %s: %d instead of %d",
				oid, i, dagfile, version2, version)
		}

		if i == 0 {
			dag.close()
			dag, err = openDAG(dagfile)
			if err != nil {
				t.Fatalf("Cannot re-open DAG file %s", dagfile)
			}
		} else if i == 1 {
			version = 888
			if err = dag.setHead(oid, version); err != nil {
				t.Fatalf("Cannot set new object head %d (%d) in DAG file %s", oid, version, dagfile)
			}
			dag.flush()
		}
	}

	dag.close()
}

// checkEndOfSync simulates and check the end-of-sync operations: clear the
// node grafting metadata and verify that it is empty and that HasConflict()
// detects this case and fails, then close the DAG.
func checkEndOfSync(d *dag, oid storage.ID) error {
	// Clear grafting info; this happens at the end of a sync log replay.
	d.clearGraft()

	// There should be no grafting info, and hasConflict() should fail.
	newHeads, grafts := d.getGraftNodes(oid)
	if newHeads != nil || grafts != nil {
		return fmt.Errorf("Object %d: graft info not cleared: newHeads (%v), grafts (%v)", oid, newHeads, grafts)
	}

	isConflict, newHead, oldHead, ancestor, errConflict := d.hasConflict(oid)
	if errConflict == nil {
		return fmt.Errorf("Object %d: conflict did not fail: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	d.close()
	return nil
}

// TestLocalUpdates tests the sync handling of initial local updates: an object
// is created (v0) and updated twice (v1, v2) on this device.  The DAG should
// show: v0 -> v1 -> v2 and the head should point to v2.
func TestLocalUpdates(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must have moved to "v2" and the parent map shows the updated DAG.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Invalid object %d head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Make sure an existing node cannot be added again.
	if err = dag.addNode(oid, 1, false, []storage.Version{0, 2}, "foobar"); err == nil {
		t.Errorf("addNode() did not fail when given an existing node")
	}

	// Make sure a new node cannot have more than 2 parents.
	if err = dag.addNode(oid, 3, false, []storage.Version{0, 1, 2}, "foobar"); err == nil {
		t.Errorf("addNode() did not fail when given 3 parents")
	}

	// Make sure a new node cannot have an invalid parent.
	if err = dag.addNode(oid, 3, false, []storage.Version{0, 555}, "foobar"); err == nil {
		t.Errorf("addNode() did not fail when using an invalid parent")
	}

	// Make sure a new root node (no parents) cannot be added once a root exists.
	// For the parents array, check both the "nil" and the empty array as input.
	if err = dag.addNode(oid, 6789, false, nil, "foobar"); err == nil {
		t.Errorf("Adding a 2nd root node (nil parents) for object %d in DAG file %s did not fail", oid, dagfile)
	}
	if err = dag.addNode(oid, 6789, false, []storage.Version{}, "foobar"); err == nil {
		t.Errorf("Adding a 2nd root node (empty parents) for object %d in DAG file %s did not fail", oid, dagfile)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteUpdates tests the sync handling of initial remote updates:
// an object is created (v0) and updated twice (v1, v2) on another device and
// we learn about it during sync.  The updated DAG should show: v0 -> v1 -> v2
// and report no conflicts with the new head pointing at v2.
func TestRemoteUpdates(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "remote-init-00.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still undefined) and the parent
	// map shows the newly grafted DAG fragment.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e == nil {
		t.Errorf("Object %d head found in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[storage.Version]struct{}{2: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[storage.Version]uint64{}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 2 && oldHead == 0 && ancestor == 0 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, e := dag.getLogrec(oid, newHead); e != nil || logrec != "logrec-02" {
		t.Errorf("Invalid logrec for newhead object %d:%d in DAG file %s: %v", oid, newHead, dagfile, logrec)
	}

	// Make sure an unknown node cannot become the new head.
	if err = dag.moveHead(oid, 55); err == nil {
		t.Errorf("moveHead() did not fail on an invalid node")
	}

	// Then we can move the head and clear the grafting data.
	if err = dag.moveHead(oid, newHead); err != nil {
		t.Errorf("Object %d cannot move head to %d in DAG file %s: %v", oid, newHead, dagfile, err)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteNoConflict tests sync of remote updates on top of a local initial
// state without conflict.  An object is created locally and updated twice
// (v0 -> v1 -> v2).  Another device, having gotten this info, makes 3 updates
// on top of that (v2 -> v3 -> v4 -> v5) and sends this info in a later sync.
// The updated DAG should show (v0 -> v1 -> v2 -> v3 -> v4 -> v5) and report
// no conflicts with the new head pointing at v5.  It should also report v2 as
// the graft point on which the new fragment (v3 -> v4 -> v5) gets attached.
func TestRemoteNoConflict(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-noconf-00.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1}, 3: {2}, 4: {3}, 5: {4}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[storage.Version]struct{}{5: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[storage.Version]uint64{2: 2}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 5 && oldHead == 2 && ancestor == 0 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, e := dag.getLogrec(oid, oldHead); e != nil || logrec != "logrec-02" {
		t.Errorf("Invalid logrec for oldhead object %d:%d in DAG file %s: %v", oid, oldHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, newHead); e != nil || logrec != "logrec-05" {
		t.Errorf("Invalid logrec for newhead object %d:%d in DAG file %s: %v", oid, newHead, dagfile, logrec)
	}

	// Then we can move the head and clear the grafting data.
	if err = dag.moveHead(oid, newHead); err != nil {
		t.Errorf("Object %d cannot move head to %d in DAG file %s: %v", oid, newHead, dagfile, err)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteConflict tests sync handling remote updates that build on the
// local initial state and trigger a conflict.  An object is created locally
// and updated twice (v0 -> v1 -> v2).  Another device, having only gotten
// the v0 -> v1 history, makes 3 updates on top of v1 (v1 -> v3 -> v4 -> v5)
// and sends this info during a later sync.  Separately, the local device
// makes a conflicting (concurrent) update v1 -> v2.  The updated DAG should
// show the branches: (v0 -> v1 -> v2) and (v0 -> v1 -> v3 -> v4 -> v5) and
// report the conflict between v2 and v5 (current and new heads).  It should
// also report v1 as the graft point and the common ancestor in the conflict.
// The conflict is resolved locally by creating v6 that is derived from both
// v2 and v5 and it becomes the new head.
func TestRemoteConflict(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-conf-00.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1}, 3: {1}, 4: {3}, 5: {4}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[storage.Version]struct{}{2: struct{}{}, 5: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[storage.Version]uint64{1: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be a conflict between v2 and v5 with v1 as ancestor.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(isConflict && newHead == 5 && oldHead == 2 && ancestor == 1 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, e := dag.getLogrec(oid, oldHead); e != nil || logrec != "logrec-02" {
		t.Errorf("Invalid logrec for oldhead object %d:%d in DAG file %s: %v", oid, oldHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, newHead); e != nil || logrec != "logrec-05" {
		t.Errorf("Invalid logrec for newhead object %d:%d in DAG file %s: %v", oid, newHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, ancestor); e != nil || logrec != "logrec-01" {
		t.Errorf("Invalid logrec for ancestor object %d:%d in DAG file %s: %v", oid, ancestor, dagfile, logrec)
	}

	// Resolve the conflict by adding a new local v6 derived from v2 and v5 (this replay moves the head).
	if err = dagReplayCommands(dag, "local-resolve-00.sync"); err != nil {
		t.Fatal(err)
	}

	// Verify that the head moved to v6 and the parent map shows the resolution.
	if head, e := dag.getHead(oid); e != nil || head != 6 {
		t.Errorf("Object %d has wrong head after conflict resolution in DAG file %s: %d", oid, dagfile, head)
	}

	exp[6] = []storage.Version{2, 5}
	pmap = dag.getParentMap(oid)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map after conflict resolution in DAG file %s: (%v) instead of (%v)",
			oid, dagfile, pmap, exp)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteConflictTwoGrafts tests sync handling remote updates that build
// on the local initial state and trigger a conflict with 2 graft points.
// An object is created locally and updated twice (v0 -> v1 -> v2).  Another
// device, first learns about v0 and makes it own conflicting update v0 -> v3.
// That remote device later learns about v1 and resolves the v1/v3 confict by
// creating v4.  Then it makes a last v4 -> v5 update -- which will conflict
// with v2 but it doesn't know that.
// Now the sync order is reversed and the local device learns all of what
// happened on the remote device.  The local DAG should get be augmented by
// a subtree with 2 graft points: v0 and v1.  It receives this new branch:
// v0 -> v3 -> v4 -> v5.  Note that v4 is also derived from v1 as a remote
// conflict resolution.  This should report a conflict between v2 and v5
// (current and new heads), with v0 and v1 as graft points, and v1 as the
// most-recent common ancestor for that conflict.  The conflict is resolved
// locally by creating v6, derived from both v2 and v5, becoming the new head.
func TestRemoteConflictTwoGrafts(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-conf-01.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1}, 3: {0}, 4: {1, 3}, 5: {4}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[storage.Version]struct{}{2: struct{}{}, 5: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[storage.Version]uint64{0: 0, 1: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be a conflict between v2 and v5 with v1 as ancestor.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(isConflict && newHead == 5 && oldHead == 2 && ancestor == 1 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, e := dag.getLogrec(oid, oldHead); e != nil || logrec != "logrec-02" {
		t.Errorf("Invalid logrec for oldhead object %d:%d in DAG file %s: %v", oid, oldHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, newHead); e != nil || logrec != "logrec-05" {
		t.Errorf("Invalid logrec for newhead object %d:%d in DAG file %s: %v", oid, newHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, ancestor); e != nil || logrec != "logrec-01" {
		t.Errorf("Invalid logrec for ancestor object %d:%d in DAG file %s: %v", oid, ancestor, dagfile, logrec)
	}

	// Resolve the conflict by adding a new local v6 derived from v2 and v5 (this replay moves the head).
	if err = dagReplayCommands(dag, "local-resolve-00.sync"); err != nil {
		t.Fatal(err)
	}

	// Verify that the head moved to v6 and the parent map shows the resolution.
	if head, e := dag.getHead(oid); e != nil || head != 6 {
		t.Errorf("Object %d has wrong head after conflict resolution in DAG file %s: %d", oid, dagfile, head)
	}

	exp[6] = []storage.Version{2, 5}
	pmap = dag.getParentMap(oid)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map after conflict resolution in DAG file %s: (%v) instead of (%v)",
			oid, dagfile, pmap, exp)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestAncestorIterator checks that the iterator goes over the correct set
// of ancestor nodes for an object given a starting node.  It should traverse
// reconvergent DAG branches only visiting each ancestor once:
// v0 -> v1 -> v2 -> v4 -> v5 -> v7 -> v8
//        |--> v3 ---|           |
//        +--> v6 ---------------+
// - Starting at v0 it should only cover v0.
// - Starting at v2 it should only cover v0-v2.
// - Starting at v5 it should only cover v0-v5.
// - Starting at v8 it should cover all nodes (v0-v8).
func TestAncestorIterator(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-01.sync"); err != nil {
		t.Fatal(err)
	}

	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	// Loop checking the iteration behavior for different starting nodes.
	for _, start := range []storage.Version{0, 2, 5, 8} {
		visitCount := make(map[storage.Version]int)
		err = dag.ancestorIter(oid, []storage.Version{start},
			func(oid storage.ID, v storage.Version, node *dagNode) error {
				visitCount[v]++
				return nil
			})

		// Check that all prior nodes are visited only once.
		for i := storage.Version(0); i < (start + 1); i++ {
			if visitCount[i] != 1 {
				t.Errorf("wrong visit count for iter on object %d node %d starting from node %d: %d instead of 1",
					oid, i, start, visitCount[i])
			}
		}
	}

	// Make sure an error in the callback is returned through the iterator.
	cbErr := errors.New("callback error")
	err = dag.ancestorIter(oid, []storage.Version{8}, func(oid storage.ID, v storage.Version, node *dagNode) error {
		if v == 0 {
			return cbErr
		}
		return nil
	})
	if err != cbErr {
		t.Errorf("wrong error returned from callback: %v instead of %v", err, cbErr)
	}

	if err = checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestPruning tests sync pruning of the DAG for an object with 3 concurrent
// updates (i.e. 2 conflict resolution convergent points).  The pruning must
// get rid of the DAG branches across the reconvergence points:
// v0 -> v1 -> v2 -> v4 -> v5 -> v7 -> v8
//        |--> v3 ---|           |
//        +--> v6 ---------------+
// By pruning at v0, nothing is deleted.
// Then by pruning at v1, only v0 is deleted.
// Then by pruning at v5, v1-v4 are deleted leaving v5 and "v6 -> v7 -> v8".
// Then by pruning at v7, v5-v6 are deleted leaving "v7 -> v8".
// Then by pruning at v8, v7 is deleted leaving v8 as the head.
// Then by pruning again at v8 nothing changes.
func TestPruning(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-01.sync"); err != nil {
		t.Fatal(err)
	}

	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1}, 3: {1}, 4: {2, 3}, 5: {4}, 6: {1}, 7: {5, 6}, 8: {7}}

	// Loop pruning at an invalid version (333) then at v0, v5, v8 and again at v8.
	testVersions := []storage.Version{333, 0, 1, 5, 7, 8, 8}
	delCounts := []int{0, 0, 1, 4, 2, 1, 0}

	for i, version := range testVersions {
		del := 0
		err = dag.prune(oid, version, func(lr string) error {
			del++
			return nil
		})

		if i == 0 && err == nil {
			t.Errorf("pruning non-existent object %d:%d did not fail in DAG file %s", oid, version, dagfile)
		} else if i > 0 && err != nil {
			t.Errorf("pruning object %d:%d failed in DAG file %s: %v", oid, version, dagfile, err)
		}

		if del != delCounts[i] {
			t.Errorf("pruning object %d:%d deleted %d log records instead of %d", oid, version, del, delCounts[i])
		}

		if head, err := dag.getHead(oid); err != nil || head != 8 {
			t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
		}

		// Remove pruned nodes from the expected parent map used to validate
		// and set the parents of the pruned node to nil.
		if version < 10 {
			for j := storage.Version(0); j < version; j++ {
				delete(exp, j)
			}
			exp[version] = nil
		}

		pmap := dag.getParentMap(oid)
		if !reflect.DeepEqual(pmap, exp) {
			t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
		}
	}

	if err = checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestPruningCallbackError tests sync pruning of the DAG when the callback
// function returns an error.  The pruning must try to delete as many nodes
// and log records as possible and properly adjust the parent pointers of
// the pruning node.  The object DAG is:
// v0 -> v1 -> v2 -> v4 -> v5 -> v7 -> v8
//        |--> v3 ---|           |
//        +--> v6 ---------------+
// By pruning at v8 and having the callback function fail for v3, all other
// nodes must be deleted and only v8 remains as the head.
func TestPruningCallbackError(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-01.sync"); err != nil {
		t.Fatal(err)
	}

	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	exp := map[storage.Version][]storage.Version{8: nil}

	// Prune at v8 with a callback function that fails for v3.
	del, expDel := 0, 8
	version := storage.Version(8)
	err = dag.prune(oid, version, func(lr string) error {
		del++
		if lr == "logrec-03" {
			return fmt.Errorf("refuse to delete %s", lr)
		}
		return nil
	})

	if err == nil {
		t.Errorf("pruning object %d:%d did not fail in DAG file %s", oid, version, dagfile)
	}
	if del != expDel {
		t.Errorf("pruning object %d:%d deleted %d log records instead of %d", oid, version, del, expDel)
	}

	if head, err := dag.getHead(oid); err != nil || head != 8 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	if err = checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestDAGCompact tests compacting of dag's kvdb file.
func TestDAGCompact(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	// Put some data in "heads" table.
	headMap := make(map[storage.ID]storage.Version)
	for i := 0; i < 10; i++ {
		// Generate a random object id in [0, 1000).
		oid, err := strToObjID(fmt.Sprintf("%d", rand.Intn(1000)))
		if err != nil {
			t.Fatal(err)
		}
		// Generate a random version number for this object.
		vers := storage.Version(rand.Intn(5000))

		// Cache this <oid,version> pair to verify with getHead().
		headMap[oid] = vers

		if err = dag.setHead(oid, vers); err != nil {
			t.Fatalf("Cannot set object head %d (%d) in DAG file %s", oid, vers, dagfile)
		}

		// Flush immediately to let the kvdb file grow.
		dag.flush()
	}

	// Put some data in "nodes" table.
	type nodeKey struct {
		oid  storage.ID
		vers storage.Version
	}
	nodeMap := make(map[nodeKey]*dagNode)
	for oid, vers := range headMap {
		// Generate a random dag node for this <oid, vers>.
		l := uint64(rand.Intn(20))
		p1 := storage.Version(rand.Intn(5000))
		p2 := storage.Version(rand.Intn(5000))
		log := fmt.Sprintf("%d", rand.Intn(1000))
		node := &dagNode{Level: l, Parents: []storage.Version{p1, p2}, Logrec: log}

		// Cache this <oid,version, dagNode> to verify with getNode().
		key := nodeKey{oid: oid, vers: vers}
		nodeMap[key] = node

		if err = dag.setNode(oid, vers, node); err != nil {
			t.Fatalf("Cannot set object %d:%d (%v) in DAG file %s", oid, vers, node, dagfile)
		}

		// Flush immediately to let the kvdb file grow.
		dag.flush()
	}

	// Get size before compaction.
	oldSize := fileSize(dagfile)
	if oldSize < 0 {
		t.Fatalf("DAG file %s not created", dagfile)
	}

	if err = dag.compact(); err != nil {
		t.Fatalf("Cannot compact DAG file %s", dagfile)
	}

	// Verify size of kvdb file is reduced.
	size := fileSize(dagfile)
	if size < 0 {
		t.Fatalf("DAG file %s not created", dagfile)
	}
	if size > oldSize {
		t.Fatalf("DAG file %s not compacted", dagfile)
	}

	// Check data exists after compaction.
	for oid, vers := range headMap {
		vers2, err := dag.getHead(oid)
		if err != nil {
			t.Errorf("Cannot find stored object head %d in DAG file %s", oid, dagfile)
		}
		if vers != vers2 {
			t.Errorf("Object %d has wrong head data in DAG file %s: %d instead of %d",
				oid, dagfile, vers2, vers)
		}
	}
	for key, node := range nodeMap {
		node2, err := dag.getNode(key.oid, key.vers)
		if err != nil || node2 == nil {
			t.Errorf("Cannot find stored object %d:%d in DAG file %s", key.oid, key.vers, dagfile)
		}
		if !reflect.DeepEqual(node, node2) {
			t.Errorf("Object %d:%d has wrong data in DAG file %s: %v instead of %v",
				key.oid, key.vers, dagfile, node2, node)
		}
	}
	dag.close()
}

// TestRemoteLinkedNoConflictSameHead tests sync of remote updates that contain
// linked nodes (conflict resolution by selecting an existing version) on top of
// a local initial state without conflict.  An object is created locally and
// updated twice (v0 -> v1 -> v2).  Another device has learned about v0, created
// (v0 -> v3), then learned about (v0 -> v1) and resolved that conflict by selecting
// v1 over v3.  Now it sends that new info (v3 and the v1/v3 link) back to the
// original (local) device.  Instead of a v2/v3 conflict, the device sees that
// v1 was chosen over v3 and resolves it as a no-conflict case.
func TestRemoteLinkedNoConflictSameHead(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-noconf-link-00.log.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0, 3}, 2: {1}, 3: {0}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[storage.Version]struct{}{2: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[storage.Version]uint64{0: 0, 3: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 2 && oldHead == 2 && ancestor == 0 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteLinkedConflict tests sync of remote updates that contain linked
// nodes (conflict resolution by selecting an existing version) on top of a local
// initial state triggering a local conflict.  An object is created locally and
// updated twice (v0 -> v1 -> v2).  Another device has along the way learned about v0,
// created (v0 -> v3), then learned about (v0 -> v1) and resolved that conflict by
// selecting v3 over v1.  Now it sends that new info (v3 and the v3/v1 link) back
// to the original (local) device.  The device sees a v2/v3 conflict.
func TestRemoteLinkedConflict(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-conf-link.log.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1}, 3: {0, 1}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[storage.Version]struct{}{2: struct{}{}, 3: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[storage.Version]uint64{0: 0, 1: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(isConflict && newHead == 3 && oldHead == 2 && ancestor == 1 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteLinkedNoConflictNewHead tests sync of remote updates that contain
// linked nodes (conflict resolution by selecting an existing version) on top of
// a local initial state without conflict, but moves the head node to a new one.
// An object is created locally and updated twice (v0 -> v1 -> v2).  Another device
// has along the way learned about v0, created (v0 -> v3), then learned about
// (v0 -> v1 -> v2) and resolved that conflict by selecting v3 over v2.  Now it
// sends that new info (v3 and the v3/v2 link) back to the original (local) device.
// The device sees that the new head v3 is "derived" from v2 thus no conflict.
func TestRemoteLinkedConflictNewHead(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-noconf-link-01.log.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1}, 3: {0, 2}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[storage.Version]struct{}{3: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[storage.Version]uint64{0: 0, 2: 2}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 3 && oldHead == 2 && ancestor == 0 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteLinkedNoConflictNewHeadOvertake tests sync of remote updates that
// contain linked nodes (conflict resolution by selecting an existing version)
// on top of a local initial state without conflict, but moves the head node
// to a new one that overtook the linked node.
// An object is created locally and updated twice (v0 -> v1 -> v2).  Another
// device has along the way learned about v0, created (v0 -> v3), then learned
// about (v0 -> v1 -> v2) and resolved that conflict by selecting v2 over v3.
// Then it creates a new update v4 from v2 (v2 -> v4).  Now it sends that new
// info (v3, the v2/v3 link, and v4) back to the original (local) device.
// The device sees that the new head v4 is "derived" from v2 thus no conflict.
func TestRemoteLinkedConflictNewHeadOvertake(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-noconf-link-02.log.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjID("12345")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[storage.Version][]storage.Version{0: nil, 1: {0}, 2: {1, 3}, 3: {0}, 4: {2}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %d parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[storage.Version]struct{}{4: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[storage.Version]uint64{0: 0, 2: 2, 3: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 4 && oldHead == 2 && ancestor == 0 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Then we can move the head and clear the grafting data.
	if err = dag.moveHead(oid, newHead); err != nil {
		t.Errorf("Object %d cannot move head to %d in DAG file %s: %v", oid, newHead, dagfile, err)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Now new info comes from another device repeating the v2/v3 link.
	// Verify that it is a NOP (no changes).
	if err = dagReplayCommands(dag, "remote-noconf-link-repeat.log.sync"); err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 4 {
		t.Errorf("Object %d has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	newHeads, grafts = dag.getGraftNodes(oid)
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %d has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts = map[storage.Version]uint64{}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %d graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if !(!isConflict && newHead == 4 && oldHead == 4 && ancestor == 0 && errConflict == nil) {
		t.Errorf("Object %d wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}
