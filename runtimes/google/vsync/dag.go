package vsync

// Veyron Sync DAG (directed acyclic graph) utility functions.
// The DAG is used to track the version history of objects in order to
// detect and resolve conflicts (concurrent changes on different devices).
//
// Terminology:
// * An object is a unique value in the Veyron Store represented by its UID.
// * As an object mutates, its version number is updated by the Store.
// * Each (object, version) tuple is represented by a node in the Sync DAG.
// * The previous version of an object is its parent in the DAG, i.e. the
//   new version is derived from that parent.
// * When there are no conflicts, the node has a single reference back to
//   a parent node.
// * When a conflict between two concurrent object versions is resolved,
//   the new version has references back to each of the two parents to
//   indicate that it is derived from both nodes.
// * During a sync operation from a source device to a target device, the
//   target receives a DAG fragment from the source.  That fragment has to
//   be incorporated (grafted) into the target device's DAG.  It may be a
//   continuation of the DAG of an object, with the attachment (graft) point
//   being the current head of DAG, in which case there are no conflicts.
//   Or the graft point(s) may be older nodes, which means the new fragment
//   is a divergence in the graph causing a conflict that must be resolved
//   in order to re-converge the two DAG fragments.
//
// In the diagrams below:
// (h) represents the head node in the local device.
// (nh) represents the new head node received from the remote device.
// (g) represents a graft node, where new nodes attach to the existing DAG.
// <- represents a derived-from mutation, i.e. a child-to-parent pointer
//
// a- No-conflict example: the new nodes (v3, v4) attach to the head node (v2).
//    In this case the new head becomes the head node, the new DAG fragment
//    being a continuation of the existing DAG.
//
//    Before:
//    v0 <- v1 <- v2(h)
//
//    Sync updates applied, no conflict detected:
//    v0 <- v1 <- v2(h,g) <- v3 <- v4 (nh)
//
//    After:
//    v0 <- v1 <- v2 <- v3 <- v4 (h)
//
// b- Conflict example: the new nodes (v3, v4) attach to an old node (v1).
//    The current head node (v2) and the new head node (v4) are divergent
//    (concurrent) mutations that need to be resolved.  The conflict
//    resolution function is passed the old head (v2), new head (v4), and
//    the common ancestor (v1) and resolves the conflict with (v5) which
//    is represented in the DAG as derived from both v2 and v4 (2 parents).
//
//    Before:
//    v0 <- v1 <- v2(h)
//
//    Sync updates applied, conflict detected (v2 not a graft node):
//    v0 <- v1(g) <- v2(h)
//                <- v3 <- v4 (nh)
//
//    After, conflict resolver creates v5 having 2 parents (v2, v4):
//    v0 <- v1(g) <- v2 <------- v5(h)
//                <- v3 <- v4 <-
//
// Note: the DAG does not grow indefinitely.  During a sync operation each
// device learns what the other device already knows -- where it's at in
// the version history for the objects.  When a device determines that all
// devices that sync an object (as per the definitions of replication groups
// in the Veyron Store) have moved past some version for that object, the
// DAG for that object can be pruned, deleting all prior (ancestor) nodes.
//
// The DAG DB contains two tables persisted to disk (nodes, heads) and
// an in-memory (ephemeral) graft map:
//   * nodes: one entry per (object, version) with references to the
//            parent node(s) it is derived from, and a reference to the
//            log record identifying that change
//   * heads: one entry per object pointing to its most recent version
//            in the nodes collection
//   * graft: during a sync operation, it tracks the nodes where the new
//            DAG fragments are attached to the existing graph for each
//            mutated object.  This map is used to determine whether a
//            conflict happened for an object and, if yes, select the most
//            recent common ancestor from these graft points to use when
//            resolving the conflict.  At the end of a sync operation the
//            graft map is destroyed.
//
// Note: for regular (no-conflict) changes, a node has a reference to
// one parent from which it was derived.  When a conflict is resolved,
// the new node has references to the two concurrent parents that triggered
// the conflict.  The states of the parents[] array are:
//   * []            The earliest/first version of an object
//   * [XYZ]         Regular non-conflict version derived from XYZ
//   * [XYZ, ABC]    Resolution version caused by XYZ-vs-ABC conflict

import (
	"container/list"
	"errors"
	"fmt"

	"veyron2/storage"
)

type dag struct {
	fname string                    // file pathname
	store *kvdb                     // underlying K/V store
	heads *kvtable                  // pointer to "heads" table in the store
	nodes *kvtable                  // pointer to "nodes" table in the store
	graft map[storage.ID]*graftInfo // in-memory state of DAG object grafting
}

type dagNode struct {
	Level   uint64            // node distance from root
	Parents []storage.Version // references to parent versions
	Logrec  string            // reference to log record change
}

type graftInfo struct {
	newNodes   map[storage.Version]bool   // set of newly added nodes during a sync
	graftNodes map[storage.Version]uint64 // set of graft nodes and their level
	newHead    storage.Version            // new (pending) head node
}

// openDAG opens or creates a DAG for the given filename.
func openDAG(filename string) (*dag, error) {
	// Open the file and create it if it does not exist.
	// Also initialize the store and its two collections.
	db, tbls, err := kvdbOpen(filename, []string{"heads", "nodes"})
	if err != nil {
		return nil, err
	}

	d := &dag{
		fname: filename,
		store: db,
		heads: tbls[0],
		nodes: tbls[1],
	}

	d.clearGraft()

	return d, nil
}

// close closes the DAG and invalidates its structure.
func (d *dag) close() {
	if d.store != nil {
		d.store.close() // this also closes the tables
	}
	*d = dag{} // zero out the DAG struct
}

// flush flushes the DAG store to disk.
func (d *dag) flush() {
	if d.store != nil {
		d.store.flush()
	}
}

// compact compacts dag's kvdb file.
func (d *dag) compact() error {
	if d.store == nil {
		return errors.New("invalid DAG")
	}
	db, tbls, err := d.store.compact(d.fname, []string{"heads", "nodes"})
	if err != nil {
		return err
	}
	d.store = db
	d.heads = tbls[0]
	d.nodes = tbls[1]
	return nil
}

// clearGraft clears the temporary in-memory grafting maps.
func (d *dag) clearGraft() {
	if d.store != nil {
		d.graft = make(map[storage.ID]*graftInfo)
	}
}

// addNode adds a new node for an object in the DAG, linking it to its parent nodes.
// It verifies that this node does not exist and that its parent nodes are valid.
// It also determines the DAG level of the node from its parent nodes (max() + 1).
//
// If the node is due to a local change (from the Watcher API), no need to
// update the grafting structure.  Otherwise the node is due to a remote change
// (from the Sync protocol) being grafted on the DAG:
// - If a parent node is not new, mark it as a DAG graft point.
// - Mark this version as a new node.
// - Update the new head node pointer of the grafted DAG.
func (d *dag) addNode(oid storage.ID, version storage.Version, remote bool, parents []storage.Version, logrec string) error {
	if d.store == nil {
		return errors.New("invalid DAG")
	}
	if parents != nil {
		if len(parents) > 2 {
			return fmt.Errorf("cannot have more than 2 parents, not %d", len(parents))
		}
		if len(parents) == 0 {
			// Replace an empty array with a nil.
			parents = nil
		}
	}

	// The new node must not exist.
	if d.hasNode(oid, version) {
		return fmt.Errorf("node %d:%d already exists in the DAG", oid, version)
	}

	// A new root node (no parents) is allowed only for new objects.
	if parents == nil {
		_, err := d.getHead(oid)
		if err == nil {
			return fmt.Errorf("cannot add another root node %d:%d for this object in the DAG", oid, version)
		}
	}

	// For a remote change, make sure the object has a graft info entry.
	// During a sync operation, each mutated object gets new nodes added
	// in its DAG.  These new nodes are either derived from nodes that
	// were previously known on this device (i.e. their parent nodes are
	// pre-existing), or they are derived from other new DAG nodes being
	// discovered during this sync (i.e. their parent nodes were also
	// just added to the DAG).
	//
	// To detect a conflict and find the most recent common ancestor to
	// pass to the conflict resolver callback, the DAG keeps track of the
	// new nodes that have old parent nodes.  These old-to-new edges are
	// the points where new DAG fragments are attached (grafted) onto the
	// existing DAG.  The old nodes are the "graft nodes" and they form
	// the set of possible common ancestors to use in case of conflict:
	// 1- A conflict happens when the current "head node" for an object
	//    is not in the set of graft nodes.  It means the object mutations
	//    were not derived from what the device knows, but where divergent
	//    changes from a prior point (from one of the graft nodes).
	// 2- The most recent common ancestor to use in resolving the conflict
	//    is the object graft node with the deepest level (furthest from
	//    the origin root node), representing the most up-to-date common
	//    knowledge between this device and the divergent changes.
	//
	// The graftInfo struct for an object is ephemeral (in-memory) and it
	// tracks the set of newly added nodes, which in turn is used to
	// detect an old-to-new edge which identifies the old node as a graft
	// node.  The graft nodes and their DAG levels are tracked in a map
	// which is used to (1) detect conflicts and (2) select the common
	// ancestor to pass to the conflict resolver.
	//
	// Note: at the end of a sync operation between 2 devices, the whole
	// graft info is cleared (Syncd calls clearGraft()) to prepare it for
	// the new pairwise sync operation.
	graft := d.graft[oid]
	if graft == nil && remote {
		graft = &graftInfo{newNodes: make(map[storage.Version]bool), graftNodes: make(map[storage.Version]uint64)}
		d.graft[oid] = graft
	}

	// Verify the parents and determine the node level.
	// Update the graft info in the DAG for this object.
	var level uint64
	for _, parent := range parents {
		node, err := d.getNode(oid, parent)
		if err != nil {
			return err
		}
		if level <= node.Level {
			level = node.Level + 1
		}
		if remote {
			// If this parent is an old node, it's a graft point in the DAG
			// and may be a common ancestor used during conflict resolution.
			if !graft.newNodes[parent] {
				graft.graftNodes[parent] = node.Level
			}
		}
	}

	if remote {
		// This new node is so far the candidate for new head version.
		graft.newNodes[version] = true
		graft.newHead = version
	}

	// Insert the new node in the kvdb.
	node := &dagNode{Level: level, Parents: parents, Logrec: logrec}
	return d.setNode(oid, version, node)
}

// hasNode returns true if the node (oid, version) exists in the DAG.
func (d *dag) hasNode(oid storage.ID, version storage.Version) bool {
	if d.store == nil {
		return false
	}
	key := objNodeKey(oid, version)
	return d.nodes.hasKey(key)
}

// moveHead moves the object head node in the DAG.
func (d *dag) moveHead(oid storage.ID, head storage.Version) error {
	if d.store == nil {
		return errors.New("invalid DAG")
	}

	// Verify that the node exists.
	if !d.hasNode(oid, head) {
		return fmt.Errorf("node %d:%d does not exist in the DAG", oid, head)
	}

	return d.setHead(oid, head)
}

// hasConflict determines if there is a conflict for this object between its
// new and old head nodes.
// - Yes: return (true, newHead, oldHead, ancestor)
// - No:  return (false, newHead, oldHead, 0)
// A conflict exists if the current (old) head node is not one of the graft
// point of the graph fragment just added.  It means the newly added object
// versions are not derived in part from this device's current knowledge.
func (d *dag) hasConflict(oid storage.ID) (isConflict bool, newHead, oldHead, ancestor storage.Version, err error) {
	if d.store == nil {
		err = errors.New("invalid DAG")
		return
	}

	graft := d.graft[oid]
	if graft == nil {
		err = fmt.Errorf("node %d has no DAG graft information", oid)
		return
	}

	newHead = graft.newHead
	oldHead, err2 := d.getHead(oid)
	if err2 != nil {
		// This is a new object not previously known on this device, so
		// it does not yet have a "head node" here.  This means all the
		// DAG updates for it are new and to taken as-is (no conflict).
		// That's why we ignore the error which is only indicating the
		// lack of a prior head node.
		return
	}

	if _, ok := graft.graftNodes[oldHead]; ok {
		return // no conflict, current head is a graft point
	}

	// There is a conflict: the best choice ancestor is the graft point
	// node with the largest level (farthest from the root).  It is
	// possible in some corner cases to have multiple graft nodes at
	// the same level.  This would still be a single conflict, but the
	// multiple same-level graft points representing equivalent conflict
	// resolutions on different devices that are now merging their
	// resolutions.  In such a case it does not matter which node is
	// chosen as the ancestor because the conflict resolver function
	// is assumed to be convergent.  However it's nicer to make that
	// selection deterministic so all devices see the same choice.
	// For this the version number is used as a tie-breaker.
	isConflict = true
	var maxLevel uint64
	for node, level := range graft.graftNodes {
		if maxLevel < level ||
			(maxLevel == level && ancestor < node) {
			maxLevel = level
			ancestor = node
		}
	}
	return
}

// ancestorIter iterates over the DAG ancestor nodes for an object in a
// breadth-first traversal starting from given version node(s).  In its
// traversal it invokes the callback function once for each node, passing
// the object ID, version number and a pointer to the dagNode.
func (d *dag) ancestorIter(oid storage.ID, startVersions []storage.Version,
	cb func(storage.ID, storage.Version, *dagNode) error) error {
	visited := make(map[storage.Version]bool)
	queue := list.New()
	for _, version := range startVersions {
		queue.PushBack(version)
		visited[version] = true
	}

	for queue.Len() > 0 {
		version := queue.Remove(queue.Front()).(storage.Version)
		node, err := d.getNode(oid, version)
		if err != nil {
			// Ignore it, the parent was previously pruned.
			continue
		}
		for _, parent := range node.Parents {
			if !visited[parent] {
				queue.PushBack(parent)
				visited[parent] = true
			}
		}
		if err = cb(oid, version, node); err != nil {
			return err
		}
	}

	return nil
}

// prune trims the DAG of an object at a given version (node) by deleting
// all its ancestor nodes, making it the new root node.  For each deleted
// node it calls the given callback function to delete its log record.
// This function should only be called when Sync determines that all devices
// that know about the object have gotten past this version.
func (d *dag) prune(oid storage.ID, version storage.Version, delLogRec func(logrec string) error) error {
	if d.store == nil {
		return errors.New("invalid DAG")
	}

	// Get the node at the pruning point and set its parents to nil.
	// It will become the oldest DAG node (root) for the object.
	node, err := d.getNode(oid, version)
	if err != nil {
		return err
	}
	if node.Parents == nil {
		// Nothing to do, this node is already the root.
		return nil
	}

	iterVersions := node.Parents

	node.Parents = nil
	if err = d.setNode(oid, version, node); err != nil {
		return err
	}

	// Delete all ancestor nodes and their log records.
	// Delete as many as possible and track the error counts.
	numNodeErrs, numLogErrs := 0, 0
	err = d.ancestorIter(oid, iterVersions, func(oid storage.ID, v storage.Version, node *dagNode) error {
		if err := delLogRec(node.Logrec); err != nil {
			numLogErrs++
		}
		if err := d.delNode(oid, v); err != nil {
			numNodeErrs++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if numNodeErrs != 0 || numLogErrs != 0 {
		return fmt.Errorf("prune failed to delete %d nodes and %d log records", numNodeErrs, numLogErrs)
	}
	return nil
}

// getLogrec returns the log record information for a given object version.
func (d *dag) getLogrec(oid storage.ID, version storage.Version) (string, error) {
	node, err := d.getNode(oid, version)
	if err != nil {
		return "", err
	}
	return node.Logrec, nil
}

// objNodeKey returns the key used to access the object node (oid, version)
// in the DAG.
func objNodeKey(oid storage.ID, version storage.Version) string {
	return fmt.Sprintf("%s:%d", oid.String(), version)
}

// setNode stores the dagNode structure for the object node (oid, version)
// in the DAG.
func (d *dag) setNode(oid storage.ID, version storage.Version, node *dagNode) error {
	if d.store == nil {
		return errors.New("invalid DAG")
	}
	key := objNodeKey(oid, version)
	return d.nodes.set(key, node)
}

// getNode retrieves the dagNode structure for the object node (oid, version)
// from the DAG.
func (d *dag) getNode(oid storage.ID, version storage.Version) (*dagNode, error) {
	if d.store == nil {
		return nil, errors.New("invalid DAG")
	}
	var node dagNode
	key := objNodeKey(oid, version)
	if err := d.nodes.get(key, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

// delNode deletes the object node (oid, version) from the DAG.
func (d *dag) delNode(oid storage.ID, version storage.Version) error {
	if d.store == nil {
		return errors.New("invalid DAG")
	}
	key := objNodeKey(oid, version)
	return d.nodes.del(key)
}

// objHeadKey returns the key used to access the object head in the DAG.
func objHeadKey(oid storage.ID) string {
	return oid.String()
}

// setHead stores version as the object head in the DAG.
func (d *dag) setHead(oid storage.ID, version storage.Version) error {
	if d.store == nil {
		return errors.New("invalid DAG")
	}
	key := objHeadKey(oid)
	return d.heads.set(key, version)
}

// getHead retrieves the object head from the DAG.
func (d *dag) getHead(oid storage.ID) (storage.Version, error) {
	if d.store == nil {
		return 0, errors.New("invalid DAG")
	}
	var version storage.Version
	key := objHeadKey(oid)
	err := d.heads.get(key, &version)
	return version, err
}

// getParentMap is a testing and debug helper function that returns for
// an object a map of all the object version in the DAG and their parents.
// The map represents the graph of the object version history.
func (d *dag) getParentMap(oid storage.ID) map[storage.Version][]storage.Version {
	parentMap := make(map[storage.Version][]storage.Version)
	var iterVersions []storage.Version

	if head, err := d.getHead(oid); err == nil {
		iterVersions = append(iterVersions, head)
	}
	if graft := d.graft[oid]; graft != nil {
		iterVersions = append(iterVersions, graft.newHead)
	}

	// Breadth-first traversal starting from the object head.
	d.ancestorIter(oid, iterVersions, func(oid storage.ID, v storage.Version, node *dagNode) error {
		parentMap[v] = node.Parents
		return nil
	})

	return parentMap
}

// getGraftNodes is a testing and debug helper function that returns for
// an object the graft information built and used during a sync operation.
// The newHead version identifies the head node reported by the other device
// during a sync operation.  The graftNodes map identifies the set of old
// nodes where the new DAG fragments were attached and their depth level
// in the DAG.
func (d *dag) getGraftNodes(oid storage.ID) (storage.Version, map[storage.Version]uint64) {
	if d.store != nil {
		if ginfo := d.graft[oid]; ginfo != nil {
			return ginfo.newHead, ginfo.graftNodes
		}
	}
	return 0, nil
}
