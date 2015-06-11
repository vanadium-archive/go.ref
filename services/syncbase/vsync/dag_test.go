// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for the Syncbase DAG.

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/store"

	"v.io/v23/context"
)

// TestSetNode tests setting and getting a DAG node.
func TestSetNode(t *testing.T) {
	svc := createService(t)
	st := svc.St()

	oid, version := "1111", "1"

	node, err := getNode(nil, st, oid, version)
	if err == nil || node != nil {
		t.Errorf("found non-existent object %s:%s: %v", oid, version, node)
	}

	if hasNode(nil, st, oid, version) {
		t.Errorf("hasNode() found non-existent object %s:%s", oid, version)
	}

	if logrec, err := getLogrec(nil, st, oid, version); err == nil || logrec != "" {
		t.Errorf("non-existent object %s:%s has a logrec: %v", oid, version, logrec)
	}

	node = &dagNode{Level: 15, Parents: []string{"444", "555"}, Logrec: "logrec-23"}

	tx := st.NewTransaction()
	if err = setNode(nil, tx, oid, version, node); err != nil {
		t.Fatalf("cannot set object %s:%s (%v): %v", oid, version, node, err)
	}
	tx.Commit()

	node2, err := getNode(nil, st, oid, version)
	if err != nil || node2 == nil {
		t.Errorf("cannot find stored object %s:%s: %v", oid, version, err)
	}

	if !hasNode(nil, st, oid, version) {
		t.Errorf("hasNode() did not find object %s:%s", oid, version)
	}

	if !reflect.DeepEqual(node, node2) {
		t.Errorf("object %s:%s has wrong data: %v instead of %v", oid, version, node2, node)
	}

	if logrec, err := getLogrec(nil, st, oid, version); err != nil || logrec != "logrec-23" {
		t.Errorf("object %s:%s has wrong logrec: %s", oid, version, logrec)
	}
}

// TestDelNode tests deleting a DAG node.
func TestDelNode(t *testing.T) {
	svc := createService(t)
	st := svc.St()

	oid, version := "2222", "2"

	node := &dagNode{Level: 123, Parents: []string{"333"}, Logrec: "logrec-789"}

	tx := st.NewTransaction()
	if err := setNode(nil, tx, oid, version, node); err != nil {
		t.Fatalf("cannot set object %s:%s (%v): %v", oid, version, node, err)
	}
	tx.Commit()

	tx = st.NewTransaction()
	if err := delNode(nil, tx, oid, version); err != nil {
		t.Fatalf("cannot delete object %s:%s: %v", oid, version, err)
	}
	tx.Commit()

	node2, err := getNode(nil, st, oid, version)
	if err == nil || node2 != nil {
		t.Errorf("found deleted object %s:%s (%v)", oid, version, node2)
	}

	if hasNode(nil, st, oid, version) {
		t.Errorf("hasNode() found deleted object %s:%s", oid, version)
	}

	if logrec, err := getLogrec(nil, st, oid, version); err == nil || logrec != "" {
		t.Errorf("deleted object %s:%s has logrec: %s", oid, version, logrec)
	}
}

// TestAddParent tests adding parents to a DAG node.
func TestAddParent(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid, version := "1234", "7"

	tx := st.NewTransaction()
	if err := s.addParent(nil, tx, oid, version, "haha", nil); err == nil {
		t.Errorf("addParent() did not fail for an unknown object %s:%s", oid, version)
	}
	tx.Abort()

	if _, err := s.dagReplayCommands(nil, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}

	node := &dagNode{Level: 15, Logrec: "logrec-22"}

	tx = st.NewTransaction()
	if err := setNode(nil, tx, oid, version, node); err != nil {
		t.Fatalf("cannot set object %s:%s (%v): %v", oid, version, node, err)
	}
	tx.Commit()

	graft := newGraft()
	tx = st.NewTransaction()
	if err := s.addParent(nil, tx, oid, version, version, graft); err == nil {
		t.Errorf("addParent() did not fail on a self-parent for object %s:%s", oid, version)
	}
	tx.Abort()

	remote := true
	expParents := []string{"4", "5", "6"}

	for _, parent := range expParents {
		tx = st.NewTransaction()
		if err := s.addParent(nil, tx, oid, version, parent, graft); err == nil {
			t.Errorf("addParent() did not reject invalid parent %s for object %s:%s",
				parent, oid, version)
		}
		tx.Abort()

		pnode := &dagNode{Level: 11, Logrec: fmt.Sprintf("logrec-%s", parent), Parents: []string{"3"}}

		tx = st.NewTransaction()
		if err := setNode(nil, tx, oid, parent, pnode); err != nil {
			t.Fatalf("cannot set parent object %s:%s (%v): %v", oid, parent, pnode, err)
		}
		tx.Commit()

		var g graftMap
		if remote {
			g = graft
		}

		// addParent() twice to verify it is idempotent.
		for i := 0; i < 2; i++ {
			tx = st.NewTransaction()
			if err := s.addParent(nil, tx, oid, version, parent, g); err != nil {
				t.Errorf("addParent() failed on parent %s, remote %t (i=%d) for %s:%s: %v",
					parent, remote, i, oid, version, err)
			}
			tx.Commit()
		}

		remote = !remote
	}

	node2, err := getNode(nil, st, oid, version)
	if err != nil || node2 == nil {
		t.Errorf("cannot find object %s:%s: %v", oid, version, err)
	}

	if !reflect.DeepEqual(node2.Parents, expParents) {
		t.Errorf("invalid parents for object %s:%s: %v instead of %v",
			oid, version, node2.Parents, expParents)
	}

	// Creating cycles should fail.
	for v := 1; v < 7; v++ {
		ver := fmt.Sprintf("%d", v)
		tx = st.NewTransaction()
		if err = s.addParent(nil, tx, oid, ver, version, nil); err == nil {
			t.Errorf("addParent() failed to reject a cycle for %s: from ancestor %s to node %s",
				oid, ver, version)
		}
		tx.Abort()
	}
}

// TestSetHead tests setting and getting a DAG head node.
func TestSetHead(t *testing.T) {
	svc := createService(t)
	st := svc.St()

	oid := "3333"

	version, err := getHead(nil, st, oid)
	if err == nil {
		t.Errorf("found non-existent object head %s:%s", oid, version)
	}

	for i := 0; i < 2; i++ {
		version = fmt.Sprintf("v%d", 555+i)

		tx := st.NewTransaction()
		if err = setHead(nil, tx, oid, version); err != nil {
			t.Fatalf("cannot set object head %s:%s (i=%d)", oid, version, i)
		}
		tx.Commit()

		version2, err := getHead(nil, st, oid)
		if err != nil {
			t.Errorf("cannot find stored object head %s (i=%d)", oid, i)
		}
		if version != version2 {
			t.Errorf("object %s has wrong head data (i=%d): %s instead of %s",
				oid, i, version2, version)
		}
	}
}

// TestLocalUpdates tests the sync handling of initial local updates: an object
// is created (v1) and updated twice (v2, v3) on this device.  The DAG should
// show: v1 -> v2 -> v3 and the head should point to v3.
func TestLocalUpdates(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid := "1234"

	if _, err := s.dagReplayCommands(nil, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}

	// The head must have moved to v3 and the parent map shows the updated DAG.
	if head, err := getHead(nil, st, oid); err != nil || head != "3" {
		t.Errorf("invalid object %s head: %s", oid, head)
	}

	pmap := getParentMap(nil, st, oid, nil)

	exp := map[string][]string{"1": nil, "2": {"1"}, "3": {"2"}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("invalid object %s parent map: (%v) instead of (%v)", oid, pmap, exp)
	}

	// Make sure an existing node cannot be added again.
	tx := st.NewTransaction()
	if err := s.addNode(nil, tx, oid, "2", "foo", false, []string{"1", "3"}, NoBatchId, nil); err == nil {
		t.Errorf("addNode() did not fail when given an existing node")
	}

	// Make sure a new node cannot have more than 2 parents.
	if err := s.addNode(nil, tx, oid, "4", "foo", false, []string{"1", "2", "3"}, NoBatchId, nil); err == nil {
		t.Errorf("addNode() did not fail when given 3 parents")
	}

	// Make sure a new node cannot have an invalid parent.
	if err := s.addNode(nil, tx, oid, "4", "foo", false, []string{"1", "555"}, NoBatchId, nil); err == nil {
		t.Errorf("addNode() did not fail when using an invalid parent")
	}

	// Make sure a new root node (no parents) cannot be added once a root exists.
	// For the parents array, check both the "nil" and the empty array as input.
	if err := s.addNode(nil, tx, oid, "6789", "foo", false, nil, NoBatchId, nil); err == nil {
		t.Errorf("adding a 2nd root node (nil parents) for object %s did not fail", oid)
	}
	if err := s.addNode(nil, tx, oid, "6789", "foo", false, []string{}, NoBatchId, nil); err == nil {
		t.Errorf("adding a 2nd root node (empty parents) for object %s did not fail", oid)
	}
	tx.Abort()
}

// TestRemoteUpdates tests the sync handling of initial remote updates:
// an object is created (v1) and updated twice (v2, v3) on another device and
// we learn about it during sync.  The updated DAG should show: v1 -> v2 -> v3
// and report no conflicts with the new head pointing at v3.
func TestRemoteUpdates(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid := "1234"

	graft, err := s.dagReplayCommands(nil, "remote-init-00.log.sync")
	if err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still undefined) and the parent
	// map shows the newly grafted DAG fragment.
	if head, err := getHead(nil, st, oid); err == nil {
		t.Errorf("object %s head found: %s", oid, head)
	}

	pmap := getParentMap(nil, st, oid, graft)

	exp := map[string][]string{"1": nil, "2": {"1"}, "3": {"2"}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("invalid object %s parent map: (%v) instead of (%v)", oid, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	g := graft[oid]

	expNewHeads := map[string]bool{"3": true}

	if !reflect.DeepEqual(g.newHeads, expNewHeads) {
		t.Errorf("object %s has invalid newHeads: (%v) instead of (%v)", oid, g.newHeads, expNewHeads)
	}

	expGrafts := map[string]uint64{}
	if !reflect.DeepEqual(g.graftNodes, expGrafts) {
		t.Errorf("invalid object %s graft: (%v) instead of (%v)", oid, g.graftNodes, expGrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := hasConflict(nil, st, oid, graft)
	if !(!isConflict && newHead == "3" && oldHead == NoVersion && ancestor == NoVersion && errConflict == nil) {
		t.Errorf("object %s wrong conflict info: flag %t, newHead %s, oldHead %s, ancestor %s, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, err := getLogrec(nil, st, oid, newHead); err != nil || logrec != "VeyronPhone:10:1:2" {
		t.Errorf("invalid logrec for newhead object %s:%s: %v", oid, newHead, logrec)
	}

	// Make sure an unknown node cannot become the new head.
	tx := st.NewTransaction()
	if err := moveHead(nil, tx, oid, "55"); err == nil {
		t.Errorf("moveHead() did not fail on an invalid node")
	}
	tx.Abort()

	// Then move the head.
	tx = st.NewTransaction()
	if err := moveHead(nil, tx, oid, newHead); err != nil {
		t.Errorf("object %s cannot move head to %s: %v", oid, newHead, err)
	}
	tx.Commit()
}

// TestRemoteNoConflict tests sync of remote updates on top of a local initial
// state without conflict.  An object is created locally and updated twice
// (v1 -> v2 -> v3).  Another device, having gotten this info, makes 3 updates
// on top of that (v3 -> v4 -> v5 -> v6) and sends this info in a later sync.
// The updated DAG should show (v1 -> v2 -> v3 -> v4 -> v5 -> v6) and report
// no conflicts with the new head pointing at v6.  It should also report v3 as
// the graft point on which the new fragment (v4 -> v5 -> v6) gets attached.
func TestRemoteNoConflict(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid := "1234"

	if _, err := s.dagReplayCommands(nil, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}
	graft, err := s.dagReplayCommands(nil, "remote-noconf-00.log.sync")
	if err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v3) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	if head, err := getHead(nil, st, oid); err != nil || head != "3" {
		t.Errorf("object %s has wrong head: %s", oid, head)
	}

	pmap := getParentMap(nil, st, oid, graft)

	exp := map[string][]string{"1": nil, "2": {"1"}, "3": {"2"}, "4": {"3"}, "5": {"4"}, "6": {"5"}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("invalid object %s parent map: (%v) instead of (%v)", oid, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	g := graft[oid]

	expNewHeads := map[string]bool{"6": true}
	if !reflect.DeepEqual(g.newHeads, expNewHeads) {
		t.Errorf("object %s has invalid newHeads: (%v) instead of (%v)", oid, g.newHeads, expNewHeads)
	}

	expGrafts := map[string]uint64{"3": 2}
	if !reflect.DeepEqual(g.graftNodes, expGrafts) {
		t.Errorf("invalid object %s graft: (%v) instead of (%v)", oid, g.graftNodes, expGrafts)
	}

	// There should be no conflict.
	isConflict, newHead, oldHead, ancestor, errConflict := hasConflict(nil, st, oid, graft)
	if !(!isConflict && newHead == "6" && oldHead == "3" && ancestor == NoVersion && errConflict == nil) {
		t.Errorf("object %s wrong conflict info: flag %t, newHead %s, oldHead %s, ancestor %s, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, err := getLogrec(nil, st, oid, oldHead); err != nil || logrec != "logrec-02" {
		t.Errorf("invalid logrec for oldhead object %s:%s: %v", oid, oldHead, logrec)
	}
	if logrec, err := getLogrec(nil, st, oid, newHead); err != nil || logrec != "VeyronPhone:10:1:2" {
		t.Errorf("invalid logrec for newhead object %s:%s: %v", oid, newHead, logrec)
	}

	// Then move the head.
	tx := st.NewTransaction()
	if err := moveHead(nil, tx, oid, newHead); err != nil {
		t.Errorf("object %s cannot move head to %s: %v", oid, newHead, err)
	}
	tx.Commit()

	// Verify that hasConflict() fails without graft data.
	isConflict, newHead, oldHead, ancestor, errConflict = hasConflict(nil, st, oid, nil)
	if errConflict == nil {
		t.Errorf("hasConflict() on %s did not fail w/o graft data: flag %t, newHead %s, oldHead %s, ancestor %s, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}
}

// TestRemoteConflict tests sync handling remote updates that build on the
// local initial state and trigger a conflict.  An object is created locally
// and updated twice (v1 -> v2 -> v3).  Another device, having only gotten
// the v1 -> v2 history, makes 3 updates on top of v2 (v2 -> v4 -> v5 -> v6)
// and sends this info during a later sync.  Separately, the local device
// makes a conflicting (concurrent) update v2 -> v3.  The updated DAG should
// show the branches: (v1 -> v2 -> v3) and (v1 -> v2 -> v4 -> v5 -> v6) and
// report the conflict between v3 and v6 (current and new heads).  It should
// also report v2 as the graft point and the common ancestor in the conflict.
// The conflict is resolved locally by creating v7 that is derived from both
// v3 and v6 and it becomes the new head.
func TestRemoteConflict(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid := "1234"

	if _, err := s.dagReplayCommands(nil, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}
	graft, err := s.dagReplayCommands(nil, "remote-conf-00.log.sync")
	if err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v3) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	if head, err := getHead(nil, st, oid); err != nil || head != "3" {
		t.Errorf("object %s has wrong head: %s", oid, head)
	}

	pmap := getParentMap(nil, st, oid, graft)

	exp := map[string][]string{"1": nil, "2": {"1"}, "3": {"2"}, "4": {"2"}, "5": {"4"}, "6": {"5"}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("invalid object %s parent map: (%v) instead of (%v)", oid, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	g := graft[oid]

	expNewHeads := map[string]bool{"3": true, "6": true}
	if !reflect.DeepEqual(g.newHeads, expNewHeads) {
		t.Errorf("object %s has invalid newHeads: (%v) instead of (%v)", oid, g.newHeads, expNewHeads)
	}

	expGrafts := map[string]uint64{"2": 1}
	if !reflect.DeepEqual(g.graftNodes, expGrafts) {
		t.Errorf("invalid object %s graft: (%v) instead of (%v)", oid, g.graftNodes, expGrafts)
	}

	// There should be a conflict between v3 and v6 with v2 as ancestor.
	isConflict, newHead, oldHead, ancestor, errConflict := hasConflict(nil, st, oid, graft)
	if !(isConflict && newHead == "6" && oldHead == "3" && ancestor == "2" && errConflict == nil) {
		t.Errorf("object %s wrong conflict info: flag %t, newHead %s, oldHead %s, ancestor %s, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, err := getLogrec(nil, st, oid, oldHead); err != nil || logrec != "logrec-02" {
		t.Errorf("invalid logrec for oldhead object %s:%s: %s", oid, oldHead, logrec)
	}
	if logrec, err := getLogrec(nil, st, oid, newHead); err != nil || logrec != "VeyronPhone:10:1:2" {
		t.Errorf("invalid logrec for newhead object %s:%s: %s", oid, newHead, logrec)
	}
	if logrec, err := getLogrec(nil, st, oid, ancestor); err != nil || logrec != "logrec-01" {
		t.Errorf("invalid logrec for ancestor object %s:%s: %s", oid, ancestor, logrec)
	}

	// Resolve the conflict by adding a new local v7 derived from v3 and v6 (this replay moves the head).
	if _, err := s.dagReplayCommands(nil, "local-resolve-00.sync"); err != nil {
		t.Fatal(err)
	}

	// Verify that the head moved to v7 and the parent map shows the resolution.
	if head, err := getHead(nil, st, oid); err != nil || head != "7" {
		t.Errorf("object %s has wrong head after conflict resolution: %s", oid, head)
	}

	exp["7"] = []string{"3", "6"}
	pmap = getParentMap(nil, st, oid, nil)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("invalid object %s parent map after conflict resolution: (%v) instead of (%v)",
			oid, pmap, exp)
	}
}

// TestRemoteConflictTwoGrafts tests sync handling remote updates that build
// on the local initial state and trigger a conflict with 2 graft points.
// An object is created locally and updated twice (v1 -> v2 -> v3).  Another
// device, first learns about v1 and makes it own conflicting update v1 -> v4.
// That remote device later learns about v2 and resolves the v2/v4 confict by
// creating v5.  Then it makes a last v5 -> v6 update -- which will conflict
// with v3 but it doesn't know that.
// Now the sync order is reversed and the local device learns all of what
// happened on the remote device.  The local DAG should get be augmented by
// a subtree with 2 graft points: v1 and v2.  It receives this new branch:
// v1 -> v4 -> v5 -> v6.  Note that v5 is also derived from v2 as a remote
// conflict resolution.  This should report a conflict between v3 and v6
// (current and new heads), with v1 and v2 as graft points, and v2 as the
// most-recent common ancestor for that conflict.  The conflict is resolved
// locally by creating v7, derived from both v3 and v6, becoming the new head.
func TestRemoteConflictTwoGrafts(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid := "1234"

	if _, err := s.dagReplayCommands(nil, "local-init-00.log.sync"); err != nil {
		t.Fatal(err)
	}
	graft, err := s.dagReplayCommands(nil, "remote-conf-01.log.sync")
	if err != nil {
		t.Fatal(err)
	}

	// The head must not have moved (i.e. still at v3) and the parent map
	// shows the newly grafted DAG fragment on top of the prior DAG.
	if head, err := getHead(nil, st, oid); err != nil || head != "3" {
		t.Errorf("object %s has wrong head: %s", oid, head)
	}

	pmap := getParentMap(nil, st, oid, graft)

	exp := map[string][]string{"1": nil, "2": {"1"}, "3": {"2"}, "4": {"1"}, "5": {"2", "4"}, "6": {"5"}}

	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("invalid object %s parent map: (%v) instead of (%v)", oid, pmap, exp)
	}

	// Verify the grafting of remote nodes.
	g := graft[oid]

	expNewHeads := map[string]bool{"3": true, "6": true}
	if !reflect.DeepEqual(g.newHeads, expNewHeads) {
		t.Errorf("object %s has invalid newHeads: (%v) instead of (%v)", oid, g.newHeads, expNewHeads)
	}

	expGrafts := map[string]uint64{"1": 0, "2": 1}
	if !reflect.DeepEqual(g.graftNodes, expGrafts) {
		t.Errorf("invalid object %s graft: (%v) instead of (%v)", oid, g.graftNodes, expGrafts)
	}

	// There should be a conflict between v3 and v6 with v2 as ancestor.
	isConflict, newHead, oldHead, ancestor, errConflict := hasConflict(nil, st, oid, graft)
	if !(isConflict && newHead == "6" && oldHead == "3" && ancestor == "2" && errConflict == nil) {
		t.Errorf("object %s wrong conflict info: flag %t, newHead %s, oldHead %s, ancestor %s, err %v",
			oid, isConflict, newHead, oldHead, ancestor, errConflict)
	}

	if logrec, err := getLogrec(nil, st, oid, oldHead); err != nil || logrec != "logrec-02" {
		t.Errorf("invalid logrec for oldhead object %s:%s: %s", oid, oldHead, logrec)
	}
	if logrec, err := getLogrec(nil, st, oid, newHead); err != nil || logrec != "VeyronPhone:10:1:1" {
		t.Errorf("invalid logrec for newhead object %s:%s: %s", oid, newHead, logrec)
	}
	if logrec, err := getLogrec(nil, st, oid, ancestor); err != nil || logrec != "logrec-01" {
		t.Errorf("invalid logrec for ancestor object %s:%s: %s", oid, ancestor, logrec)
	}

	// Resolve the conflict by adding a new local v7 derived from v3 and v6 (this replay moves the head).
	if _, err := s.dagReplayCommands(nil, "local-resolve-00.sync"); err != nil {
		t.Fatal(err)
	}

	// Verify that the head moved to v7 and the parent map shows the resolution.
	if head, err := getHead(nil, st, oid); err != nil || head != "7" {
		t.Errorf("object %s has wrong head after conflict resolution: %s", oid, head)
	}

	exp["7"] = []string{"3", "6"}
	pmap = getParentMap(nil, st, oid, nil)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("invalid object %s parent map after conflict resolution: (%v) instead of (%v)",
			oid, pmap, exp)
	}
}

// TestAncestorIterator checks that the iterator goes over the correct set
// of ancestor nodes for an object given a starting node.  It should traverse
// reconvergent DAG branches only visiting each ancestor once:
// v1 -> v2 -> v3 -> v5 -> v6 -> v8 -> v9
//        |--> v4 ---|           |
//        +--> v7 ---------------+
// - Starting at v1 it should only cover v1.
// - Starting at v3 it should only cover v1-v3.
// - Starting at v6 it should only cover v1-v6.
// - Starting at v9 it should cover all nodes (v1-v9).
func TestAncestorIterator(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid := "1234"

	if _, err := s.dagReplayCommands(nil, "local-init-01.sync"); err != nil {
		t.Fatal(err)
	}

	// Loop checking the iteration behavior for different starting nodes.
	for _, start := range []int{1, 3, 6, 9} {
		visitCount := make(map[string]int)
		vstart := fmt.Sprintf("%d", start)
		forEachAncestor(nil, st, oid, []string{vstart}, func(v string, nd *dagNode) error {
			visitCount[v]++
			return nil
		})

		// Check that all prior nodes are visited only once.
		for i := 1; i < (start + 1); i++ {
			vv := fmt.Sprintf("%d", i)
			if visitCount[vv] != 1 {
				t.Errorf("wrong visit count on object %s:%s starting from %s: %d instead of 1",
					oid, vv, vstart, visitCount[vv])
			}
		}
	}

	// Make sure an error in the callback is returned.
	cbErr := errors.New("callback error")
	err := forEachAncestor(nil, st, oid, []string{"9"}, func(v string, nd *dagNode) error {
		if v == "1" {
			return cbErr
		}
		return nil
	})
	if err != cbErr {
		t.Errorf("wrong error returned from callback: %v instead of %v", err, cbErr)
	}
}

// TestPruning tests sync pruning of the DAG for an object with 3 concurrent
// updates (i.e. 2 conflict resolution convergent points).  The pruning must
// get rid of the DAG branches across the reconvergence points:
// v1 -> v2 -> v3 -> v5 -> v6 -> v8 -> v9
//        |--> v4 ---|           |
//        +--> v7 ---------------+
// By pruning at v1, nothing is deleted.
// Then by pruning at v2, only v1 is deleted.
// Then by pruning at v6, v2-v5 are deleted leaving v6 and "v7 -> v8 -> v9".
// Then by pruning at v8, v6-v7 are deleted leaving "v8 -> v9".
// Then by pruning at v9, v8 is deleted leaving v9 as the head.
// Then by pruning again at v9 nothing changes.
func TestPruning(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid := "1234"

	if _, err := s.dagReplayCommands(nil, "local-init-01.sync"); err != nil {
		t.Fatal(err)
	}

	exp := map[string][]string{"1": nil, "2": {"1"}, "3": {"2"}, "4": {"2"}, "5": {"3", "4"}, "6": {"5"}, "7": {"2"}, "8": {"6", "7"}, "9": {"8"}}

	// Loop pruning at an invalid version (333) then at different valid versions.
	testVersions := []string{"333", "1", "2", "6", "8", "9", "9"}
	delCounts := []int{0, 0, 1, 4, 2, 1, 0}
	which := "prune-snip-"
	remain := 9

	for i, version := range testVersions {
		batches := newBatchPruning()
		tx := st.NewTransaction()
		del := 0
		err := prune(nil, tx, oid, version, batches,
			func(ctx *context.T, tx store.StoreReadWriter, lr string) error {
				del++
				return nil
			})
		tx.Commit()

		if i == 0 && err == nil {
			t.Errorf("pruning non-existent object %s:%s did not fail", oid, version)
		} else if i > 0 && err != nil {
			t.Errorf("pruning object %s:%s failed: %v", oid, version, err)
		}

		if del != delCounts[i] {
			t.Errorf("pruning object %s:%s deleted %d log records instead of %d",
				oid, version, del, delCounts[i])
		}

		which += "*"
		remain -= del

		if head, err := getHead(nil, st, oid); err != nil || head != "9" {
			t.Errorf("object %s has wrong head: %s", oid, head)
		}

		tx = st.NewTransaction()
		err = pruneDone(nil, tx, batches)
		if err != nil {
			t.Errorf("pruneDone() failed: %v", err)
		}
		tx.Commit()

		// Remove pruned nodes from the expected parent map used to validate
		// and set the parents of the pruned node to nil.
		intVersion, err := strconv.ParseInt(version, 10, 32)
		if err != nil {
			t.Errorf("invalid version: %s", version)
		}

		if intVersion < 10 {
			for j := int64(0); j < intVersion; j++ {
				delete(exp, fmt.Sprintf("%d", j))
			}
			exp[version] = nil
		}

		pmap := getParentMap(nil, st, oid, nil)
		if !reflect.DeepEqual(pmap, exp) {
			t.Errorf("invalid object %s parent map: (%v) instead of (%v)", oid, pmap, exp)
		}
	}
}

// TestPruningCallbackError tests sync pruning of the DAG when the callback
// function returns an error.  The pruning must try to delete as many nodes
// and log records as possible and properly adjust the parent pointers of
// the pruning node.  The object DAG is:
// v1 -> v2 -> v3 -> v5 -> v6 -> v8 -> v9
//        |--> v4 ---|           |
//        +--> v7 ---------------+
// By pruning at v9 and having the callback function fail for v4, all other
// nodes must be deleted and only v9 remains as the head.
func TestPruningCallbackError(t *testing.T) {
	svc := createService(t)
	st := svc.St()
	s := svc.sync

	oid := "1234"

	if _, err := s.dagReplayCommands(nil, "local-init-01.sync"); err != nil {
		t.Fatal(err)
	}

	exp := map[string][]string{"9": nil}

	// Prune at v9 with a callback function that fails for v4.
	del, expDel := 0, 8
	version := "9"

	batches := newBatchPruning()
	tx := st.NewTransaction()
	err := prune(nil, tx, oid, version, batches,
		func(ctx *context.T, tx store.StoreReadWriter, lr string) error {
			del++
			if lr == "logrec-03" {
				return fmt.Errorf("refuse to delete %s", lr)
			}
			return nil
		})
	tx.Commit()

	if err == nil {
		t.Errorf("pruning object %s:%s did not fail", oid, version)
	}
	if del != expDel {
		t.Errorf("pruning object %s:%s deleted %d log records instead of %d", oid, version, del, expDel)
	}

	tx = st.NewTransaction()
	err = pruneDone(nil, tx, batches)
	if err != nil {
		t.Errorf("pruneDone() failed: %v", err)
	}
	tx.Commit()

	if head, err := getHead(nil, st, oid); err != nil || head != version {
		t.Errorf("object %s has wrong head: %s", oid, head)
	}

	pmap := getParentMap(nil, st, oid, nil)
	if !reflect.DeepEqual(pmap, exp) {
		t.Errorf("invalid object %s parent map: (%v) instead of (%v)", oid, pmap, exp)
	}
}
