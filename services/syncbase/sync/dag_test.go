// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for the Veyron Sync DAG component.

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"v.io/x/ref/lib/stats"
)

// dagFilename generates a filename for a temporary (per unit test) DAG file.
// Do not replace this function with TempFile because TempFile creates the new
// file and the tests must verify that the DAG can create a non-existing file.
func dagFilename() string {
	return fmt.Sprintf("%s/sync_dag_test_%d_%d", os.TempDir(), os.Getpid(), time.Now().UnixNano())
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

	fsize := getFileSize(dagfile)
	if fsize < 0 {
		//t.Fatalf("DAG file %s not created", dagfile)
	}

	dag.flush()
	oldfsize := fsize
	fsize = getFileSize(dagfile)
	if fsize <= oldfsize {
		//t.Fatalf("DAG file %s not flushed", dagfile)
	}

	dag.close()

	dag, err = openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot re-open existing DAG file %s", dagfile)
	}

	oldfsize = fsize
	fsize = getFileSize(dagfile)
	if fsize != oldfsize {
		t.Fatalf("DAG file %s size changed across re-open", dagfile)
	}

	dag.close()
	dag.close() // multiple closes should be a safe NOP

	fsize = getFileSize(dagfile)
	if fsize != oldfsize {
		t.Fatalf("DAG file %s size changed across close", dagfile)
	}

	// Fail opening a DAG in a non-existent directory.
	_, err = openDAG("/not/really/there/junk.dag")
	if err == nil {
		//t.Fatalf("openDAG() did not fail when using a bad pathname")
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

	oid, err := strToObjId("6789")
	if err != nil {
		t.Error(err)
	}

	validateError := func(err error, funcName string) {
		if err == nil || err.Error() != "invalid DAG" {
			t.Errorf("%s() did not fail on a closed DAG: %v", funcName, err)
		}
	}

	err = dag.addNode(oid, 4, false, false, []Version{2, 3}, "foobar", NoTxId)
	validateError(err, "addNode")

	err = dag.moveHead(oid, 4)
	validateError(err, "moveHead")

	_, _, _, _, err = dag.hasConflict(oid)
	validateError(err, "hasConflict")

	_, err = dag.getLogrec(oid, 4)
	validateError(err, "getLogrec")

	err = dag.prune(oid, 4, func(lr string) error {
		return nil
	})
	validateError(err, "prune")

	err = dag.pruneAll(oid, func(lr string) error {
		return nil
	})
	validateError(err, "pruneAll")

	err = dag.pruneDone()
	validateError(err, "pruneDone")

	node := &dagNode{Level: 15, Parents: []Version{444, 555}, Logrec: "logrec-23"}
	err = dag.setNode(oid, 4, node)
	validateError(err, "setNode")

	_, err = dag.getNode(oid, 4)
	validateError(err, "getNode")

	err = dag.delNode(oid, 4)
	validateError(err, "delNode")

	err = dag.addParent(oid, 4, 2, true)
	validateError(err, "addParent")

	err = dag.setHead(oid, 4)
	validateError(err, "setHead")

	_, err = dag.getHead(oid)
	validateError(err, "getHead")

	err = dag.delHead(oid)
	validateError(err, "delHead")

	if tid := dag.addNodeTxStart(NoTxId); tid != NoTxId {
		t.Errorf("addNodeTxStart() did not fail on a closed DAG: TxId %v", tid)
	}

	err = dag.addNodeTxEnd(1, 1)
	validateError(err, "addNodeTxEnd")

	err = dag.setTransaction(1, nil)
	validateError(err, "setTransaction")

	_, err = dag.getTransaction(1)
	validateError(err, "getTransaction")

	err = dag.delTransaction(1)
	validateError(err, "delTransaction")

	err = dag.setPrivNode(oid, nil)
	validateError(err, "setPrivNode")

	_, err = dag.getPrivNode(oid)
	validateError(err, "getPrivNode")

	err = dag.delPrivNode(oid)
	validateError(err, "delPrivNode")

	// These calls should be harmless NOPs.
	dag.clearGraft()
	dag.clearTxGC()
	dag.dump()
	dag.flush()
	dag.close()
	if dag.hasNode(oid, 4) {
		t.Errorf("hasNode() found an object on a closed DAG")
	}
	if dag.hasDeletedDescendant(oid, 3) {
		t.Errorf("hasDeletedDescendant() returned true on a closed DAG")
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

	version := Version(0)
	oid, err := strToObjId("1111")
	if err != nil {
		t.Fatal(err)
	}

	node, err := dag.getNode(oid, version)
	if err == nil || node != nil {
		t.Errorf("Found non-existent object %v:%d in DAG file %s: %v", oid, version, dagfile, node)
	}

	if dag.hasNode(oid, version) {
		t.Errorf("hasNode() found non-existent object %v:%d in DAG file %s", oid, version, dagfile)
	}

	if logrec, err := dag.getLogrec(oid, version); err == nil || logrec != "" {
		t.Errorf("Non-existent object %v:%d has a logrec in DAG file %s: %v", oid, version, dagfile, logrec)
	}

	node = &dagNode{Level: 15, Parents: []Version{444, 555}, Logrec: "logrec-23"}
	if err = dag.setNode(oid, version, node); err != nil {
		t.Fatalf("Cannot set object %v:%d (%v) in DAG file %s", oid, version, node, dagfile)
	}

	for i := 0; i < 2; i++ {
		node2, err := dag.getNode(oid, version)
		if err != nil || node2 == nil {
			t.Errorf("Cannot find stored object %v:%d (i=%d) in DAG file %s", oid, version, i, dagfile)
		}

		if !dag.hasNode(oid, version) {
			t.Errorf("hasNode() did not find object %v:%d (i=%d) in DAG file %s", oid, version, i, dagfile)
		}

		if !reflect.DeepEqual(node, node2) {
			t.Errorf("Object %v:%d has wrong data (i=%d) in DAG file %s: %v instead of %v",
				oid, version, i, dagfile, node2, node)
		}

		if logrec, err := dag.getLogrec(oid, version); err != nil || logrec != "logrec-23" {
			t.Errorf("Object %v:%d has wrong logrec (i=%d) in DAG file %s: %v",
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

	version := Version(1)
	oid, err := strToObjId("2222")
	if err != nil {
		t.Fatal(err)
	}

	node := &dagNode{Level: 123, Parents: []Version{333}, Logrec: "logrec-789"}
	if err = dag.setNode(oid, version, node); err != nil {
		t.Fatalf("Cannot set object %v:%d (%v) in DAG file %s", oid, version, node, dagfile)
	}

	dag.flush()

	err = dag.delNode(oid, version)
	if err != nil {
		t.Fatalf("Cannot delete object %v:%d in DAG file %s", oid, version, dagfile)
	}

	dag.flush()

	for i := 0; i < 2; i++ {
		node2, err := dag.getNode(oid, version)
		if err == nil || node2 != nil {
			t.Errorf("Found deleted object %v:%d (%v) (i=%d) in DAG file %s", oid, version, node2, i, dagfile)
		}

		if dag.hasNode(oid, version) {
			t.Errorf("hasNode() found deleted object %v:%d (i=%d) in DAG file %s", oid, version, i, dagfile)
		}

		if logrec, err := dag.getLogrec(oid, version); err == nil || logrec != "" {
			t.Errorf("Deleted object %v:%d (i=%d) has logrec in DAG file %s: %v", oid, version, i, dagfile, logrec)
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

	version := Version(7)
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if err = dag.addParent(oid, version, 1, true); err == nil {
		t.Errorf("addParent() did not fail for an unknown object %v:%d in DAG file %s", oid, version, dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}

	node := &dagNode{Level: 15, Logrec: "logrec-22"}
	if err = dag.setNode(oid, version, node); err != nil {
		t.Fatalf("Cannot set object %v:%d (%v) in DAG file %s", oid, version, node, dagfile)
	}

	if err = dag.addParent(oid, version, version, true); err == nil {
		t.Errorf("addParent() did not fail on a self-parent for object %v:%d in DAG file %s", oid, version, dagfile)
	}

	for _, parent := range []Version{4, 5, 6} {
		if err = dag.addParent(oid, version, parent, true); err == nil {
			t.Errorf("addParent() did not reject invalid parent %d for object %v:%d in DAG file %s",
				parent, oid, version, dagfile)
		}

		pnode := &dagNode{Level: 11, Logrec: fmt.Sprintf("logrec-%d", parent), Parents: []Version{3}}
		if err = dag.setNode(oid, parent, pnode); err != nil {
			t.Fatalf("Cannot set parent object %v:%d (%v) in DAG file %s", oid, parent, pnode, dagfile)
		}

		remote := parent%2 == 0
		for i := 0; i < 2; i++ {
			if err = dag.addParent(oid, version, parent, remote); err != nil {
				t.Errorf("addParent() failed on parent %d, remote %t (i=%d) for object %v:%d in DAG file %s: %v",
					parent, remote, i, oid, version, dagfile, err)
			}
		}
	}

	node2, err := dag.getNode(oid, version)
	if err != nil || node2 == nil {
		t.Errorf("Cannot find stored object %v:%d in DAG file %s", oid, version, dagfile)
	}

	expParents := []Version{4, 5, 6}
	if !reflect.DeepEqual(node2.Parents, expParents) {
		t.Errorf("invalid parents for object %v:%d in DAG file %s: %v instead of %v",
			oid, version, dagfile, node2.Parents, expParents)
	}

	// Creating cycles should fail.
	for v := Version(1); v < version; v++ {
		if err = dag.addParent(oid, v, version, false); err == nil {
			t.Errorf("addParent() failed to reject a cycle for object %v: from ancestor %d to node %d in DAG file %s",
				oid, v, version, dagfile)
		}
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

	oid, err := strToObjId("3333")
	if err != nil {
		t.Fatal(err)
	}

	version, err := dag.getHead(oid)
	if err == nil {
		t.Errorf("Found non-existent object head %v in DAG file %s: %d", oid, dagfile, version)
	}

	version = 555
	if err = dag.setHead(oid, version); err != nil {
		t.Fatalf("Cannot set object head %v (%d) in DAG file %s", oid, version, dagfile)
	}

	dag.flush()

	for i := 0; i < 3; i++ {
		version2, err := dag.getHead(oid)
		if err != nil {
			t.Errorf("Cannot find stored object head %v (i=%d) in DAG file %s", oid, i, dagfile)
		}
		if version != version2 {
			t.Errorf("Object %v has wrong head data (i=%d) in DAG file %s: %d instead of %d",
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
				t.Fatalf("Cannot set new object head %v (%d) in DAG file %s", oid, version, dagfile)
			}
			dag.flush()
		}
	}

	dag.close()
}

// checkEndOfSync simulates and check the end-of-sync operations: clear the
// node grafting metadata and verify that it is empty and that HasConflict()
// detects this case and fails, then close the DAG.
func checkEndOfSync(d *dag, oid ObjId) error {
	// Clear grafting info; this happens at the end of a sync log replay.
	d.clearGraft()

	// There should be no grafting or transaction info, and hasConflict() should fail.
	newHeads, grafts := d.getGraftNodes(oid)
	if newHeads != nil || grafts != nil {
		return fmt.Errorf("Object %v: graft info not cleared: newHeads (%v), grafts (%v)", oid, newHeads, grafts)
	}

	if n := len(d.txSet); n != 0 {
		return fmt.Errorf("transaction set not empty: %d entries found", n)
	}

	isConflict, newHead, oldHead, ancestor, errConflict := d.hasConflict(oid)
	if errConflict == nil {
		return fmt.Errorf("Object %v: conflict did not fail: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	d.dump()
	d.close()
	return nil
}

// checkDAGStats verifies the DAG stats counters.
func checkDAGStats(t *testing.T, which string, numObj, numNode, numTx, numPriv int64) {
	if num, err := stats.Value(statsNumDagObj); err != nil || num != numObj {
		t.Errorf("num-dag-objects (%s): got %v (err: %v) instead of %v", which, num, err, numObj)
	}
	if num, err := stats.Value(statsNumDagNode); err != nil || num != numNode {
		t.Errorf("num-dag-nodes (%s): got %v (err: %v) instead of %v", which, num, err, numNode)
	}
	if num, err := stats.Value(statsNumDagTx); err != nil || num != numTx {
		t.Errorf("num-dag-tx (%s): got %v (err: %v) instead of %v", which, num, err, numTx)
	}
	if num, err := stats.Value(statsNumDagPrivNode); err != nil || num != numPriv {
		t.Errorf("num-dag-privnodes (%s): got %v (err: %v) instead of %v", which, num, err, numPriv)
	}
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
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Invalid object %v head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{0: nil, 1: {0}, 2: {1}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Make sure an existing node cannot be added again.
	if err = dag.addNode(oid, 1, false, false, []Version{0, 2}, "foobar", NoTxId); err == nil {
		t.Errorf("addNode() did not fail when given an existing node")
	}

	// Make sure a new node cannot have more than 2 parents.
	if err = dag.addNode(oid, 3, false, false, []Version{0, 1, 2}, "foobar", NoTxId); err == nil {
		t.Errorf("addNode() did not fail when given 3 parents")
	}

	// Make sure a new node cannot have an invalid parent.
	if err = dag.addNode(oid, 3, false, false, []Version{0, 555}, "foobar", NoTxId); err == nil {
		t.Errorf("addNode() did not fail when using an invalid parent")
	}

	// Make sure a new root node (no parents) cannot be added once a root exists.
	// For the parents array, check both the "nil" and the empty array as input.
	if err = dag.addNode(oid, 6789, false, false, nil, "foobar", NoTxId); err == nil {
		t.Errorf("Adding a 2nd root node (nil parents) for object %v in DAG file %s did not fail", oid, dagfile)
	}
	if err = dag.addNode(oid, 6789, false, false, []Version{}, "foobar", NoTxId); err == nil {
		t.Errorf("Adding a 2nd root node (empty parents) for object %v in DAG file %s did not fail", oid, dagfile)
	}

	checkDAGStats(t, "local-update", 1, 3, 0, 0)

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
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e == nil {
		t.Errorf("Object %v head found in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{0: nil, 1: {0}, 2: {1}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[Version]struct{}{2: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[Version]uint64{}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 2 && oldHead == 0 && ancestor == 0 && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, e := dag.getLogrec(oid, newHead); e != nil || logrec != "logrec-02" {
		t.Errorf("Invalid logrec for newhead object %v:%d in DAG file %s: %v", oid, newHead, dagfile, logrec)
	}

	// Make sure an unknown node cannot become the new head.
	if err = dag.moveHead(oid, 55); err == nil {
		t.Errorf("moveHead() did not fail on an invalid node")
	}

	// Then we can move the head and clear the grafting data.
	if err = dag.moveHead(oid, newHead); err != nil {
		t.Errorf("Object %v cannot move head to %d in DAG file %s: %v", oid, newHead, dagfile, err)
	}

	checkDAGStats(t, "remote-update", 1, 3, 0, 0)

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
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{0: nil, 1: {0}, 2: {1}, 3: {2}, 4: {3}, 5: {4}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[Version]struct{}{5: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[Version]uint64{2: 2}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 5 && oldHead == 2 && ancestor == 0 && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, e := dag.getLogrec(oid, oldHead); e != nil || logrec != "logrec-02" {
		t.Errorf("Invalid logrec for oldhead object %v:%d in DAG file %s: %v", oid, oldHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, newHead); e != nil || logrec != "logrec-05" {
		t.Errorf("Invalid logrec for newhead object %v:%d in DAG file %s: %v", oid, newHead, dagfile, logrec)
	}

	// Then we can move the head and clear the grafting data.
	if err = dag.moveHead(oid, newHead); err != nil {
		t.Errorf("Object %v cannot move head to %d in DAG file %s: %v", oid, newHead, dagfile, err)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() on %v did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	checkDAGStats(t, "remote-noconf", 1, 6, 0, 0)

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
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{0: nil, 1: {0}, 2: {1}, 3: {1}, 4: {3}, 5: {4}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[Version]struct{}{2: struct{}{}, 5: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[Version]uint64{1: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be a conflict between v2 and v5 with v1 as ancestor.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(isConflict && newHead == 5 && oldHead == 2 && ancestor == 1 && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, e := dag.getLogrec(oid, oldHead); e != nil || logrec != "logrec-02" {
		t.Errorf("Invalid logrec for oldhead object %v:%d in DAG file %s: %v", oid, oldHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, newHead); e != nil || logrec != "logrec-05" {
		t.Errorf("Invalid logrec for newhead object %v:%d in DAG file %s: %v", oid, newHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, ancestor); e != nil || logrec != "logrec-01" {
		t.Errorf("Invalid logrec for ancestor object %v:%d in DAG file %s: %v", oid, ancestor, dagfile, logrec)
	}

	checkDAGStats(t, "remote-conf-pre", 1, 6, 0, 0)

	// Resolve the conflict by adding a new local v6 derived from v2 and v5 (this replay moves the head).
	if err = dagReplayCommands(dag, "local-resolve-00.sync"); err != nil {
		t.Fatal(err)
	}

	// Verify that the head moved to v6 and the parent map shows the resolution.
	if head, e := dag.getHead(oid); e != nil || head != 6 {
		t.Errorf("Object %v has wrong head after conflict resolution in DAG file %s: %d", oid, dagfile, head)
	}

	exp[6] = []Version{2, 5}
	pmap = dag.getParentMap(oid)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map after conflict resolution in DAG file %s: (%v) instead of (%v)",
			oid, dagfile, pmap, exp)
	}

	checkDAGStats(t, "remote-conf-post", 1, 7, 0, 0)

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
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 2 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{0: nil, 1: {0}, 2: {1}, 3: {0}, 4: {1, 3}, 5: {4}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[Version]struct{}{2: struct{}{}, 5: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[Version]uint64{0: 0, 1: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be a conflict between v2 and v5 with v1 as ancestor.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(isConflict && newHead == 5 && oldHead == 2 && ancestor == 1 && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, e := dag.getLogrec(oid, oldHead); e != nil || logrec != "logrec-02" {
		t.Errorf("Invalid logrec for oldhead object %v:%d in DAG file %s: %v", oid, oldHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, newHead); e != nil || logrec != "logrec-05" {
		t.Errorf("Invalid logrec for newhead object %v:%d in DAG file %s: %v", oid, newHead, dagfile, logrec)
	}
	if logrec, e := dag.getLogrec(oid, ancestor); e != nil || logrec != "logrec-01" {
		t.Errorf("Invalid logrec for ancestor object %v:%d in DAG file %s: %v", oid, ancestor, dagfile, logrec)
	}

	checkDAGStats(t, "remote-conf2-pre", 1, 6, 0, 0)

	// Resolve the conflict by adding a new local v6 derived from v2 and v5 (this replay moves the head).
	if err = dagReplayCommands(dag, "local-resolve-00.sync"); err != nil {
		t.Fatal(err)
	}

	// Verify that the head moved to v6 and the parent map shows the resolution.
	if head, e := dag.getHead(oid); e != nil || head != 6 {
		t.Errorf("Object %v has wrong head after conflict resolution in DAG file %s: %d", oid, dagfile, head)
	}

	exp[6] = []Version{2, 5}
	pmap = dag.getParentMap(oid)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map after conflict resolution in DAG file %s: (%v) instead of (%v)",
			oid, dagfile, pmap, exp)
	}

	checkDAGStats(t, "remote-conf2-post", 1, 7, 0, 0)

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

	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	// Loop checking the iteration behavior for different starting nodes.
	for _, start := range []Version{0, 2, 5, 8} {
		visitCount := make(map[Version]int)
		err = dag.ancestorIter(oid, []Version{start},
			func(oid ObjId, v Version, node *dagNode) error {
				visitCount[v]++
				return nil
			})

		// Check that all prior nodes are visited only once.
		for i := Version(0); i < (start + 1); i++ {
			if visitCount[i] != 1 {
				t.Errorf("wrong visit count for iter on object %v node %d starting from node %d: %d instead of 1",
					oid, i, start, visitCount[i])
			}
		}
	}

	// Make sure an error in the callback is returned through the iterator.
	cbErr := errors.New("callback error")
	err = dag.ancestorIter(oid, []Version{8}, func(oid ObjId, v Version, node *dagNode) error {
		if v == 0 {
			return cbErr
		}
		return nil
	})
	if err != cbErr {
		t.Errorf("wrong error returned from callback: %v instead of %v", err, cbErr)
	}

	checkDAGStats(t, "ancestor-iter", 1, 9, 0, 0)

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

	checkDAGStats(t, "prune-init", 1, 9, 0, 0)

	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	exp := map[Version][]Version{0: nil, 1: {0}, 2: {1}, 3: {1}, 4: {2, 3}, 5: {4}, 6: {1}, 7: {5, 6}, 8: {7}}

	// Loop pruning at an invalid version (333) then at v0, v5, v8 and again at v8.
	testVersions := []Version{333, 0, 1, 5, 7, 8, 8}
	delCounts := []int{0, 0, 1, 4, 2, 1, 0}
	which := "prune-snip-"
	remain := 9

	for i, version := range testVersions {
		del := 0
		err = dag.prune(oid, version, func(lr string) error {
			del++
			return nil
		})

		if i == 0 && err == nil {
			t.Errorf("pruning non-existent object %v:%d did not fail in DAG file %s", oid, version, dagfile)
		} else if i > 0 && err != nil {
			t.Errorf("pruning object %v:%d failed in DAG file %s: %v", oid, version, dagfile, err)
		}

		if del != delCounts[i] {
			t.Errorf("pruning object %v:%d deleted %d log records instead of %d", oid, version, del, delCounts[i])
		}

		which += "*"
		remain -= del
		checkDAGStats(t, which, 1, int64(remain), 0, 0)

		if head, err := dag.getHead(oid); err != nil || head != 8 {
			t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
		}

		err = dag.pruneDone()
		if err != nil {
			t.Errorf("pruneDone() failed in DAG file %s: %v", dagfile, err)
		}

		// Remove pruned nodes from the expected parent map used to validate
		// and set the parents of the pruned node to nil.
		if version < 10 {
			for j := Version(0); j < version; j++ {
				delete(exp, j)
			}
			exp[version] = nil
		}

		pmap := dag.getParentMap(oid)
		if !reflect.DeepEqual(pmap, exp) {
			t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
		}
	}

	checkDAGStats(t, "prune-end", 1, 1, 0, 0)

	err = dag.pruneAll(oid, func(lr string) error {
		return nil
	})
	if err != nil {
		t.Errorf("pruneAll() for object %v failed in DAG file %s: %v", oid, dagfile, err)
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

	checkDAGStats(t, "prune-cb-init", 1, 9, 0, 0)

	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	exp := map[Version][]Version{8: nil}

	// Prune at v8 with a callback function that fails for v3.
	del, expDel := 0, 8
	version := Version(8)
	err = dag.prune(oid, version, func(lr string) error {
		del++
		if lr == "logrec-03" {
			return fmt.Errorf("refuse to delete %s", lr)
		}
		return nil
	})

	if err == nil {
		t.Errorf("pruning object %v:%d did not fail in DAG file %s", oid, version, dagfile)
	}
	if del != expDel {
		t.Errorf("pruning object %v:%d deleted %d log records instead of %d", oid, version, del, expDel)
	}

	err = dag.pruneDone()
	if err != nil {
		t.Errorf("pruneDone() failed in DAG file %s: %v", dagfile, err)
	}

	if head, err := dag.getHead(oid); err != nil || head != 8 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	checkDAGStats(t, "prune-cb-end", 1, 1, 0, 0)

	if err = checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteLinkedNoConflictSameHead tests sync of remote updates that contain
// linked nodes (conflict resolution by selecting an existing version) on top of
// a local initial state without conflict.  An object is created locally and
// updated twice (v1 -> v2 -> v3).  Another device has learned about v1, created
// (v1 -> v4), then learned about (v1 -> v2) and resolved that conflict by selecting
// v2 over v4.  Now it sends that new info (v4 and the v2/v4 link) back to the
// original (local) device.  Instead of a v3/v4 conflict, the device sees that
// v2 was chosen over v4 and resolves it as a no-conflict case.
func TestRemoteLinkedNoConflictSameHead(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-noconf-link-00.log.sync"); err != nil {
		t.Fatal(err)
	}

	checkDAGStats(t, "linked-noconf", 1, 4, 0, 0)

	// The head must not have moved (i.e. still at v3) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 3 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{1: nil, 2: {1, 4}, 3: {2}, 4: {1}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[Version]struct{}{3: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[Version]uint64{1: 0, 4: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 3 && oldHead == 3 && ancestor == NoVersion && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() on %v did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteLinkedConflict tests sync of remote updates that contain linked
// nodes (conflict resolution by selecting an existing version) on top of a local
// initial state triggering a local conflict.  An object is created locally and
// updated twice (v1 -> v2 -> v3).  Another device has along the way learned about v1,
// created (v1 -> v4), then learned about (v1 -> v2) and resolved that conflict by
// selecting v4 over v2.  Now it sends that new info (v4 and the v4/v2 link) back
// to the original (local) device.  The device sees a v3/v4 conflict.
func TestRemoteLinkedConflict(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-conf-link.log.sync"); err != nil {
		t.Fatal(err)
	}

	checkDAGStats(t, "linked-conf", 1, 4, 0, 0)

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 3 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{1: nil, 2: {1}, 3: {2}, 4: {1, 2}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[Version]struct{}{3: struct{}{}, 4: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[Version]uint64{1: 0, 2: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be a conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(isConflict && newHead == 4 && oldHead == 3 && ancestor == 2 && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() on %v did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteLinkedNoConflictNewHead tests sync of remote updates that contain
// linked nodes (conflict resolution by selecting an existing version) on top of
// a local initial state without conflict, but moves the head node to a new one.
// An object is created locally and updated twice (v1 -> v2 -> v3).  Another device
// has along the way learned about v1, created (v1 -> v4), then learned about
// (v1 -> v2 -> v3) and resolved that conflict by selecting v4 over v3.  Now it
// sends that new info (v4 and the v4/v3 link) back to the original (local) device.
// The device sees that the new head v4 is "derived" from v3 thus no conflict.
func TestRemoteLinkedConflictNewHead(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-noconf-link-01.log.sync"); err != nil {
		t.Fatal(err)
	}

	checkDAGStats(t, "linked-conf2", 1, 4, 0, 0)

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 3 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{1: nil, 2: {1}, 3: {2}, 4: {1, 3}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[Version]struct{}{4: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[Version]uint64{1: 0, 3: 2}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 4 && oldHead == 3 && ancestor == NoVersion && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() on %v did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
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
// An object is created locally and updated twice (v1 -> v2 -> v3).  Another
// device has along the way learned about v1, created (v1 -> v4), then learned
// about (v1 -> v2 -> v3) and resolved that conflict by selecting v3 over v4.
// Then it creates a new update v5 from v3 (v3 -> v5).  Now it sends that new
// info (v4, the v3/v4 link, and v5) back to the original (local) device.
// The device sees that the new head v5 is "derived" from v3 thus no conflict.
func TestRemoteLinkedConflictNewHeadOvertake(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}
	if err = dagReplayCommands(dag, "remote-noconf-link-02.log.sync"); err != nil {
		t.Fatal(err)
	}

	checkDAGStats(t, "linked-conf3-pre", 1, 5, 0, 0)

	// The head must not have moved (i.e. still at v2) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 3 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	pmap := dag.getParentMap(oid)

	exp := map[Version][]Version{1: nil, 2: {1}, 3: {2, 4}, 4: {1}, 5: {3}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("Invalid object %v parent map in DAG file %s: (%v) instead of (%v)", oid, dagfile, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	newHeads, grafts := dag.getGraftNodes(oid)

	expNewHeads := map[Version]struct{}{5: struct{}{}}
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts := map[Version]uint64{1: 0, 3: 2, 4: 1}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := dag.hasConflict(oid)
	if !(!isConflict && newHead == 5 && oldHead == 3 && ancestor == NoVersion && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Then we can move the head and clear the grafting data.
	if err = dag.moveHead(oid, newHead); err != nil {
		t.Errorf("Object %v cannot move head to %d in DAG file %s: %v", oid, newHead, dagfile, err)
	}

	// Clear the grafting data and verify that hasConflict() fails without it.
	dag.clearGraft()
	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if errConflict == nil {
		t.Errorf("hasConflict() on %v did not fail w/o graft info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	// Now new info comes from another device repeating the v2/v3 link.
	// Verify that it is a NOP (no changes).
	if err = dagReplayCommands(dag, "remote-noconf-link-repeat.log.sync"); err != nil {
		t.Fatal(err)
	}

	if head, e := dag.getHead(oid); e != nil || head != 5 {
		t.Errorf("Object %v has wrong head in DAG file %s: %d", oid, dagfile, head)
	}

	newHeads, grafts = dag.getGraftNodes(oid)
	if !reflect.DeepEqual(newHeads, expNewHeads) {
		t.Errorf("Object %v has invalid newHeads in DAG file %s: (%v) instead of (%v)", oid, dagfile, newHeads, expNewHeads)
	}

	expgrafts = map[Version]uint64{}
	if !reflect.DeepEqual(grafts, expgrafts) {
		t.Errorf("Invalid object %v graft in DAG file %s: (%v) instead of (%v)", oid, dagfile, grafts, expgrafts)
	}

	isConflict, newHead, oldHead, ancestor, errConflict = dag.hasConflict(oid)
	if !(!isConflict && newHead == 5 && oldHead == 5 && ancestor == NoVersion && errConflict == nil) {
		t.Errorf("Object %v wrong conflict info: flag %t, newHead %d, oldHead %d, ancestor %d, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	checkDAGStats(t, "linked-conf3-post", 1, 5, 0, 0)

	if err := checkEndOfSync(dag, oid); err != nil {
		t.Fatal(err)
	}
}

// TestAddNodeTransactional tests adding multiple DAG nodes grouped within a transaction.
func TestAddNodeTransactional(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-02.sync"); err != nil {
		t.Fatal(err)
	}

	checkDAGStats(t, "add-tx-init", 3, 5, 0, 0)

	oid_a, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}
	oid_b, err := strToObjId("6789")
	if err != nil {
		t.Fatal(err)
	}
	oid_c, err := strToObjId("2222")
	if err != nil {
		t.Fatal(err)
	}

	// Verify NoTxId is reported as an error.
	if err := dag.addNodeTxEnd(NoTxId, 0); err == nil {
		t.Errorf("addNodeTxEnd() did not fail for invalid 'NoTxId' value")
	}
	if _, err := dag.getTransaction(NoTxId); err == nil {
		t.Errorf("getTransaction() did not fail for invalid 'NoTxId' value")
	}
	if err := dag.setTransaction(NoTxId, nil); err == nil {
		t.Errorf("setTransaction() did not fail for invalid 'NoTxId' value")
	}
	if err := dag.delTransaction(NoTxId); err == nil {
		t.Errorf("delTransaction() did not fail for invalid 'NoTxId' value")
	}

	// Mutate 2 objects within a transaction.
	tid_1 := dag.addNodeTxStart(NoTxId)
	if tid_1 == NoTxId {
		t.Fatal("Cannot start 1st DAG addNode() transaction")
	}
	if err := dag.addNodeTxEnd(tid_1, 0); err == nil {
		t.Errorf("addNodeTxEnd() did not fail for a zero-count transaction")
	}

	txSt, ok := dag.txSet[tid_1]
	if !ok {
		t.Errorf("Transactions state for Tx ID %v not found in DAG file %s", tid_1, dagfile)
	}
	if n := len(txSt.TxMap); n != 0 {
		t.Errorf("Transactions map for Tx ID %v has length %d instead of 0 in DAG file %s", tid_1, n, dagfile)
	}

	if err := dag.addNode(oid_a, 3, false, false, []Version{2}, "logrec-a-03", tid_1); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_a, tid_1, dagfile, err)
	}

	if tTmp := dag.addNodeTxStart(tid_1); tTmp != tid_1 {
		t.Fatal("restarting transaction failed")
	}

	if err := dag.addNode(oid_b, 3, false, false, []Version{2}, "logrec-b-03", tid_1); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_b, tid_1, dagfile, err)
	}

	// At the same time mutate the 3rd object in another transaction.
	tid_2 := dag.addNodeTxStart(NoTxId)
	if tid_2 == NoTxId {
		t.Fatal("Cannot start 2nd DAG addNode() transaction")
	}

	txSt, ok = dag.txSet[tid_2]
	if !ok {
		t.Errorf("Transactions state for Tx ID %v not found in DAG file %s", tid_2, dagfile)
	}
	if n := len(txSt.TxMap); n != 0 {
		t.Errorf("Transactions map for Tx ID %v has length %d instead of 0 in DAG file %s", tid_2, n, dagfile)
	}

	if err := dag.addNode(oid_c, 2, false, false, []Version{1}, "logrec-c-02", tid_2); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_c, tid_2, dagfile, err)
	}

	// Verify the in-memory transaction sets constructed.
	txSt, ok = dag.txSet[tid_1]
	if !ok {
		t.Errorf("Transactions state for Tx ID %v not found in DAG file %s", tid_1, dagfile)
	}
	expTxSt := &dagTxState{dagTxMap{oid_a: 3, oid_b: 3}, 0}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state for Tx ID %v in DAG file %s: %v instead of %v", tid_1, dagfile, txSt, expTxSt)
	}

	txSt, ok = dag.txSet[tid_2]
	if !ok {
		t.Errorf("Transactions state for Tx ID %v not found in DAG file %s", tid_2, dagfile)
	}
	expTxSt = &dagTxState{dagTxMap{oid_c: 2}, 0}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state for Tx ID %v in DAG file %s: %v instead of %v", tid_2, dagfile, txSt, expTxSt)
	}

	// Verify failing to use a Tx ID not returned by addNodeTxStart().
	bad_tid := tid_1 + 1
	for bad_tid == NoTxId || bad_tid == tid_2 {
		bad_tid++
	}

	if err := dag.addNode(oid_c, 3, false, false, []Version{2}, "logrec-c-03", bad_tid); err == nil {
		t.Errorf("addNode() did not fail on object %v for a bad Tx ID %v in DAG file %s", oid_c, bad_tid, dagfile)
	}
	if err := dag.addNodeTxEnd(bad_tid, 1); err == nil {
		t.Errorf("addNodeTxEnd() did not fail for a bad Tx ID %v in DAG file %s", bad_tid, dagfile)
	}

	// End the 1st transaction and verify the in-memory and in-DAG data.
	if err := dag.addNodeTxEnd(tid_1, 2); err != nil {
		t.Errorf("Cannot addNodeTxEnd() for Tx ID %v in DAG file %s: %v", tid_1, dagfile, err)
	}

	checkDAGStats(t, "add-tx-1", 3, 8, 1, 0)

	if _, ok = dag.txSet[tid_1]; ok {
		t.Errorf("Transactions state for Tx ID %v still exists in DAG file %s", tid_1, dagfile)
	}

	txSt, err = dag.getTransaction(tid_1)
	if err != nil {
		t.Errorf("Cannot getTransaction() for Tx ID %v in DAG file %s: %v", tid_1, dagfile, err)
	}

	expTxSt = &dagTxState{dagTxMap{oid_a: 3, oid_b: 3}, 2}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state from DAG storage for Tx ID %v in DAG file %s: %v instead of %v",
			tid_1, dagfile, txSt, expTxSt)
	}

	txSt, ok = dag.txSet[tid_2]
	if !ok {
		t.Errorf("Transactions state for Tx ID %v not found in DAG file %s", tid_2, dagfile)
	}

	expTxSt = &dagTxState{dagTxMap{oid_c: 2}, 0}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state for Tx ID %v in DAG file %s: %v instead of %v", tid_2, dagfile, txSt, expTxSt)
	}

	// End the 2nd transaction and re-verify the in-memory and in-DAG data.
	if err := dag.addNodeTxEnd(tid_2, 1); err != nil {
		t.Errorf("Cannot addNodeTxEnd() for Tx ID %v in DAG file %s: %v", tid_2, dagfile, err)
	}

	checkDAGStats(t, "add-tx-2", 3, 8, 2, 0)

	if _, ok = dag.txSet[tid_2]; ok {
		t.Errorf("Transactions state for Tx ID %v still exists in DAG file %s", tid_2, dagfile)
	}

	txSt, err = dag.getTransaction(tid_2)
	if err != nil {
		t.Errorf("Cannot getTransaction() for Tx ID %v in DAG file %s: %v", tid_2, dagfile, err)
	}

	expTxSt = &dagTxState{dagTxMap{oid_c: 2}, 1}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state for Tx ID %v in DAG file %s: %v instead of %v", tid_2, dagfile, txSt, expTxSt)
	}

	if n := len(dag.txSet); n != 0 {
		t.Errorf("Transaction sets in-memory: %d entries found, should be empty in DAG file %s", n, dagfile)
	}

	// Test incrementally filling up a transaction.
	tid_3 := TxId(100)
	if _, ok = dag.txSet[tid_3]; ok {
		t.Errorf("Transactions state for Tx ID %v found in DAG file %s", tid_3, dagfile)
	}

	if tTmp := dag.addNodeTxStart(tid_3); tTmp != tid_3 {
		t.Fatalf("Cannot start transaction %v", tid_3)
	}

	txSt, ok = dag.txSet[tid_3]
	if !ok {
		t.Errorf("Transactions state for Tx ID %v not found in DAG file %s", tid_3, dagfile)
	}
	if n := len(txSt.TxMap); n != 0 {
		t.Errorf("Transactions map for Tx ID %v has length %d instead of 0 in DAG file %s", tid_3, n, dagfile)
	}

	if err := dag.addNode(oid_a, 4, false, false, []Version{3}, "logrec-a-04", tid_3); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_a, tid_3, dagfile, err)
	}

	if err := dag.addNodeTxEnd(tid_3, 2); err != nil {
		t.Errorf("Cannot addNodeTxEnd() for Tx ID %v in DAG file %s: %v", tid_3, dagfile, err)
	}

	checkDAGStats(t, "add-tx-3", 3, 9, 3, 0)

	if _, ok = dag.txSet[tid_3]; ok {
		t.Errorf("Transactions state for Tx ID %v still exists in DAG file %s", tid_3, dagfile)
	}

	txSt, err = dag.getTransaction(tid_3)
	if err != nil {
		t.Errorf("Cannot getTransaction() for Tx ID %v in DAG file %s: %v", tid_3, dagfile, err)
	}

	expTxSt = &dagTxState{dagTxMap{oid_a: 4}, 2}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state from DAG storage for Tx ID %v in DAG file %s: %v instead of %v",
			tid_3, dagfile, txSt, expTxSt)
	}

	if tTmp := dag.addNodeTxStart(tid_3); tTmp != tid_3 {
		t.Fatalf("Cannot start transaction %v", tid_3)
	}

	txSt, ok = dag.txSet[tid_3]
	if !ok {
		t.Errorf("Transactions state for Tx ID %v not found in DAG file %s", tid_3, dagfile)
	}

	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state from DAG storage for Tx ID %v in DAG file %s: %v instead of %v",
			tid_3, dagfile, txSt, expTxSt)
	}

	if err := dag.addNode(oid_b, 4, false, false, []Version{3}, "logrec-b-04", tid_3); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_b, tid_3, dagfile, err)
	}

	if err := dag.addNodeTxEnd(tid_3, 3); err == nil {
		t.Errorf("addNodeTxEnd() didn't fail for Tx ID %v in DAG file %s: %v", tid_3, dagfile, err)
	}

	if err := dag.addNodeTxEnd(tid_3, 2); err != nil {
		t.Errorf("Cannot addNodeTxEnd() for Tx ID %v in DAG file %s: %v", tid_3, dagfile, err)
	}

	checkDAGStats(t, "add-tx-4", 3, 10, 3, 0)

	txSt, err = dag.getTransaction(tid_3)
	if err != nil {
		t.Errorf("Cannot getTransaction() for Tx ID %v in DAG file %s: %v", tid_3, dagfile, err)
	}

	expTxSt = &dagTxState{dagTxMap{oid_a: 4, oid_b: 4}, 2}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state from DAG storage for Tx ID %v in DAG file %s: %v instead of %v",
			tid_3, dagfile, txSt, expTxSt)
	}

	// Get the 3 new nodes from the DAG and verify their Tx IDs.
	node, err := dag.getNode(oid_a, 3)
	if err != nil {
		t.Errorf("Cannot find object %v:3 in DAG file %s: %v", oid_a, dagfile, err)
	}
	if node.TxId != tid_1 {
		t.Errorf("Invalid TxId for object %v:3 in DAG file %s: %v instead of %v", oid_a, dagfile, node.TxId, tid_1)
	}
	node, err = dag.getNode(oid_a, 4)
	if err != nil {
		t.Errorf("Cannot find object %v:4 in DAG file %s: %v", oid_a, dagfile, err)
	}
	if node.TxId != tid_3 {
		t.Errorf("Invalid TxId for object %v:4 in DAG file %s: %v instead of %v", oid_a, dagfile, node.TxId, tid_3)
	}
	node, err = dag.getNode(oid_b, 3)
	if err != nil {
		t.Errorf("Cannot find object %v:3 in DAG file %s: %v", oid_b, dagfile, err)
	}
	if node.TxId != tid_1 {
		t.Errorf("Invalid TxId for object %v:3 in DAG file %s: %v instead of %v", oid_b, dagfile, node.TxId, tid_1)
	}
	node, err = dag.getNode(oid_b, 4)
	if err != nil {
		t.Errorf("Cannot find object %v:4 in DAG file %s: %v", oid_b, dagfile, err)
	}
	if node.TxId != tid_3 {
		t.Errorf("Invalid TxId for object %v:4 in DAG file %s: %v instead of %v", oid_b, dagfile, node.TxId, tid_3)
	}
	node, err = dag.getNode(oid_c, 2)
	if err != nil {
		t.Errorf("Cannot find object %v:2 in DAG file %s: %v", oid_c, dagfile, err)
	}
	if node.TxId != tid_2 {
		t.Errorf("Invalid TxId for object %v:2 in DAG file %s: %v instead of %v", oid_c, dagfile, node.TxId, tid_2)
	}

	for _, oid := range []ObjId{oid_a, oid_b, oid_c} {
		if err := checkEndOfSync(dag, oid); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPruningTransactions tests pruning DAG nodes grouped within transactions.
func TestPruningTransactions(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-02.sync"); err != nil {
		t.Fatal(err)
	}

	checkDAGStats(t, "prune-tx-init", 3, 5, 0, 0)

	oid_a, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}
	oid_b, err := strToObjId("6789")
	if err != nil {
		t.Fatal(err)
	}
	oid_c, err := strToObjId("2222")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate objects in 2 transactions then add non-transactional mutations
	// to act as the pruning points.  Before pruning the DAG is:
	// a1 -- a2 -- (a3) --- a4
	// b1 -- b2 -- (b3) -- (b4) -- b5
	// c1 ---------------- (c2)
	// Now by pruning at (a4, b5, c2), the new DAG should be:
	// a4
	// b5
	// (c2)
	// Transaction 1 (a3, b3) gets deleted, but transaction 2 (b4, c2) still
	// has (c2) dangling waiting for a future pruning.
	tid_1 := dag.addNodeTxStart(NoTxId)
	if tid_1 == NoTxId {
		t.Fatal("Cannot start 1st DAG addNode() transaction")
	}
	if err := dag.addNode(oid_a, 3, false, false, []Version{2}, "logrec-a-03", tid_1); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_a, tid_1, dagfile, err)
	}
	if err := dag.addNode(oid_b, 3, false, false, []Version{2}, "logrec-b-03", tid_1); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_b, tid_1, dagfile, err)
	}
	if err := dag.addNodeTxEnd(tid_1, 2); err != nil {
		t.Errorf("Cannot addNodeTxEnd() for Tx ID %v in DAG file %s: %v", tid_1, dagfile, err)
	}

	checkDAGStats(t, "prune-tx-1", 3, 7, 1, 0)

	tid_2 := dag.addNodeTxStart(NoTxId)
	if tid_2 == NoTxId {
		t.Fatal("Cannot start 2nd DAG addNode() transaction")
	}
	if err := dag.addNode(oid_b, 4, false, false, []Version{3}, "logrec-b-04", tid_2); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_b, tid_2, dagfile, err)
	}
	if err := dag.addNode(oid_c, 2, false, false, []Version{1}, "logrec-c-02", tid_2); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_c, tid_2, dagfile, err)
	}
	if err := dag.addNodeTxEnd(tid_2, 2); err != nil {
		t.Errorf("Cannot addNodeTxEnd() for Tx ID %v in DAG file %s: %v", tid_2, dagfile, err)
	}

	checkDAGStats(t, "prune-tx-2", 3, 9, 2, 0)

	if err := dag.addNode(oid_a, 4, false, false, []Version{3}, "logrec-a-04", NoTxId); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_a, tid_1, dagfile, err)
	}
	if err := dag.addNode(oid_b, 5, false, false, []Version{4}, "logrec-b-05", NoTxId); err != nil {
		t.Errorf("Cannot addNode() on object %v and Tx ID %v in DAG file %s: %v", oid_b, tid_2, dagfile, err)
	}

	if err = dag.moveHead(oid_a, 4); err != nil {
		t.Errorf("Object %v cannot move head in DAG file %s: %v", oid_a, dagfile, err)
	}
	if err = dag.moveHead(oid_b, 5); err != nil {
		t.Errorf("Object %v cannot move head in DAG file %s: %v", oid_b, dagfile, err)
	}
	if err = dag.moveHead(oid_c, 2); err != nil {
		t.Errorf("Object %v cannot move head in DAG file %s: %v", oid_c, dagfile, err)
	}

	checkDAGStats(t, "prune-tx-3", 3, 11, 2, 0)

	// Verify the transaction sets.
	txSt, err := dag.getTransaction(tid_1)
	if err != nil {
		t.Errorf("Cannot getTransaction() for Tx ID %v in DAG file %s: %v", tid_1, dagfile, err)
	}

	expTxSt := &dagTxState{dagTxMap{oid_a: 3, oid_b: 3}, 2}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state from DAG storage for Tx ID %v in DAG file %s: %v instead of %v",
			tid_1, dagfile, txSt, expTxSt)
	}

	txSt, err = dag.getTransaction(tid_2)
	if err != nil {
		t.Errorf("Cannot getTransaction() for Tx ID %v in DAG file %s: %v", tid_2, dagfile, err)
	}

	expTxSt = &dagTxState{dagTxMap{oid_b: 4, oid_c: 2}, 2}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state for Tx ID %v in DAG file %s: %v instead of %v", tid_2, dagfile, txSt, expTxSt)
	}

	// Prune the 3 objects at their head nodes.
	for _, oid := range []ObjId{oid_a, oid_b, oid_c} {
		head, err := dag.getHead(oid)
		if err != nil {
			t.Errorf("Cannot getHead() on object %v in DAG file %s: %v", oid, dagfile, err)
		}
		err = dag.prune(oid, head, func(lr string) error {
			return nil
		})
		if err != nil {
			t.Errorf("Cannot prune() on object %v in DAG file %s: %v", oid, dagfile, err)
		}
	}

	if err = dag.pruneDone(); err != nil {
		t.Errorf("pruneDone() failed in DAG file %s: %v", dagfile, err)
	}

	if n := len(dag.txGC); n != 0 {
		t.Errorf("Transaction GC map not empty after pruneDone() in DAG file %s: %d", dagfile, n)
	}

	// Verify that Tx-1 was deleted and Tx-2 still has c2 in it.
	checkDAGStats(t, "prune-tx-4", 3, 3, 1, 0)

	txSt, err = dag.getTransaction(tid_1)
	if err == nil {
		t.Errorf("getTransaction() did not fail for Tx ID %v in DAG file %s: %v", tid_1, dagfile, txSt)
	}

	txSt, err = dag.getTransaction(tid_2)
	if err != nil {
		t.Errorf("Cannot getTransaction() for Tx ID %v in DAG file %s: %v", tid_2, dagfile, err)
	}

	expTxSt = &dagTxState{dagTxMap{oid_c: 2}, 2}
	if !reflect.DeepEqual(txSt, expTxSt) {
		t.Errorf("Invalid transaction state for Tx ID %v in DAG file %s: %v instead of %v", tid_2, dagfile, txSt, expTxSt)
	}

	// Add c3 as a new head and prune at that point.  This should GC Tx-2.
	if err := dag.addNode(oid_c, 3, false, false, []Version{2}, "logrec-c-03", NoTxId); err != nil {
		t.Errorf("Cannot addNode() on object %v in DAG file %s: %v", oid_c, dagfile, err)
	}
	if err = dag.moveHead(oid_c, 3); err != nil {
		t.Errorf("Object %v cannot move head in DAG file %s: %v", oid_c, dagfile, err)
	}

	checkDAGStats(t, "prune-tx-5", 3, 4, 1, 0)

	err = dag.prune(oid_c, 3, func(lr string) error {
		return nil
	})
	if err != nil {
		t.Errorf("Cannot prune() on object %v in DAG file %s: %v", oid_c, dagfile, err)
	}
	if err = dag.pruneDone(); err != nil {
		t.Errorf("pruneDone() #2 failed in DAG file %s: %v", dagfile, err)
	}
	if n := len(dag.txGC); n != 0 {
		t.Errorf("Transaction GC map not empty after pruneDone() in DAG file %s: %d", dagfile, n)
	}

	checkDAGStats(t, "prune-tx-6", 3, 3, 0, 0)

	txSt, err = dag.getTransaction(tid_2)
	if err == nil {
		t.Errorf("getTransaction() did not fail for Tx ID %v in DAG file %s: %v", tid_2, dagfile, txSt)
	}

	for _, oid := range []ObjId{oid_a, oid_b, oid_c} {
		if err := checkEndOfSync(dag, oid); err != nil {
			t.Fatal(err)
		}
	}
}

// TestHasDeletedDescendant tests lookup of DAG deleted nodes descending from a given node.
func TestHasDeletedDescendant(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	if err = dagReplayCommands(dag, "local-init-03.sync"); err != nil {
		t.Fatal(err)
	}

	oid, err := strToObjId("1234")
	if err != nil {
		t.Fatal(err)
	}

	// Delete node v3 to create a dangling parent link from v7 (increase code coverage).
	if err = dag.delNode(oid, 3); err != nil {
		t.Errorf("cannot delete node %v:3 in DAG file %s: %v", oid, dagfile, err)
	}

	type hasDelDescTest struct {
		node   Version
		result bool
	}
	tests := []hasDelDescTest{
		{NoVersion, false},
		{999, false},
		{1, true},
		{2, true},
		{3, false},
		{4, false},
		{5, false},
		{6, false},
		{7, false},
		{8, false},
	}

	for _, test := range tests {
		result := dag.hasDeletedDescendant(oid, test.node)
		if result != test.result {
			t.Errorf("hasDeletedDescendant() for node %d in DAG file %s: %v instead of %v",
				test.node, dagfile, result, test.result)
		}
	}

	dag.close()
}

// TestPrivNode tests access to the private nodes table in a DAG.
func TestPrivNode(t *testing.T) {
	dagfile := dagFilename()
	defer os.Remove(dagfile)

	dag, err := openDAG(dagfile)
	if err != nil {
		t.Fatalf("Cannot open new DAG file %s", dagfile)
	}

	oid, err := strToObjId("2222")
	if err != nil {
		t.Fatal(err)
	}

	priv, err := dag.getPrivNode(oid)
	if err == nil || priv != nil {
		t.Errorf("Found non-existing private object %v in DAG file %s: %v, err %v", oid, dagfile, priv, err)
	}

	priv = &privNode{
		//Mutation: &raw.Mutation{ID: oid, PriorVersion: 0x0, Version: 0x55104dc76695721d, Value: "value-foobar"},
		PathIDs: []ObjId{oid, ObjId("haha"), ObjId("foobar")},
		TxId:    56789,
	}

	if err = dag.setPrivNode(oid, priv); err != nil {
		t.Fatalf("Cannot set private object %v (%v) in DAG file %s: %v", oid, priv, dagfile, err)
	}

	checkDAGStats(t, "priv-1", 0, 0, 0, 1)

	priv2, err := dag.getPrivNode(oid)
	if err != nil {
		t.Fatalf("Cannot get private object %v from DAG file %s: %v", oid, dagfile, err)
	}
	if !reflect.DeepEqual(priv2, priv) {
		t.Errorf("Private object %v has wrong data in DAG file %s: %v instead of %v", oid, dagfile, priv2, priv)
	}

	//priv.Mutation.PriorVersion = priv.Mutation.Version
	//priv.Mutation.Version = 0x55555ddddd345abc
	//priv.Mutation.Value = "value-new"
	priv.TxId = 98765

	if err = dag.setPrivNode(oid, priv); err != nil {
		t.Fatalf("Cannot overwrite private object %v (%v) in DAG file %s: %v", oid, priv, dagfile, err)
	}

	checkDAGStats(t, "priv-1", 0, 0, 0, 1)

	priv2, err = dag.getPrivNode(oid)
	if err != nil {
		t.Fatalf("Cannot get updated private object %v from DAG file %s: %v", oid, dagfile, err)
	}
	if !reflect.DeepEqual(priv2, priv) {
		t.Errorf("Private object %v has wrong data post-update in DAG file %s: %v instead of %v", oid, dagfile, priv2, priv)
	}

	err = dag.delPrivNode(oid)
	if err != nil {
		t.Fatalf("Cannot delete private object %v in DAG file %s: %v", oid, dagfile, err)
	}

	checkDAGStats(t, "priv-1", 0, 0, 0, 0)

	priv3, err := dag.getPrivNode(oid)
	if err == nil || priv3 != nil {
		t.Errorf("Found deleted private object %v in DAG file %s: %v, err %v", oid, dagfile, priv3, err)
	}

	dag.close()
}
