package vsync

// Garbage collection (GC) in sync reclaims space occupied by sync's
// data structures when possible. For its operation, sync keeps every
// version of every object produced locally and remotely in its dag
// and log data structures. Keeping these versions indefinitely is not
// feasible given space constraints on any device. Thus to reclaim
// space, a GC thread periodically checks to see if any state can be
// deleted. GC looks for generations that every device in the system
// already knows about and deletes state belonging to those
// generations. Since every device in the system already knows about
// these generations, it is safe to delete them. Newly added devices
// will only get state starting from the generations not yet
// reclaimed. Policies are needed to handle devices that were part of
// the system, but are no longer available. Such devices will prevent
// GC from moving forward since they will not request new generations.
//
// GC in sync happens in 3 stages:
// ** reclamation phase
// ** object pruning phase
// ** online consistency check phase
//
// Reclamation phase: GC learns of the state of the other devices when
// it talks to those devices to obtain missing updates
// (initiator). The generation vectors of these devices are stored in
// the device table. In the reclamation phase, we go through the
// generation vectors of all the devices and compute the maximum
// generation of each device known to every other device. This maximum
// generation for each device is stored in the reclaim generation
// vector. We then iterate through each generation between the old
// reclaim vector to the new reclaim vector, and create for each
// object belonging to those generations, the history of versions that
// will be reclaimed and the most recent version that can be
// reclaimed.
//
// Object pruning phase: In this phase, for an object marked for
// reclamation, we prune its dag starting from the most recent version
// that is being reclaimed and delete all the versions that are
// older. As we prune the dag, we also delete the corresponding log
// records and update the generation metadata. Note that since the
// actual deletes proceed object by object, the generations will start
// to have missing log records, and we use the generation metadata to
// ensure that the generation deletes are tracked accurately. Thus,
// the decision of what to prune happens top down using generation
// information, while the actual pruning happens bottom up from the
// dag. Pruning bottom up ensures that the object dags are consistent.
//
// Online consistency check phase: GC stages need write access to the
// sync data structures since they perform delete operations. Hence,
// GC is executed under a write lock and excludes other goroutines in
// syncd. In order to control the impact of GC on foreground
// operations, GC is designed to be incremental in its execution. Once
// objects are marked for deletion, only a small batch of objects are
// pruned and persisted and the lock is released. Thus objects are
// incrementally deleted, a small batch every
// garbageCollectInterval. To persist the changes from a round of GC,
// we immediately persist the new reclaim vector. For the batch of
// objects gc'ed in a round, we also persist their deletions. However,
// if the system restarts or crashes when all the dirty objects from a
// round of GC are not processed, there will be state from generations
// older than the reclaim vector still persisted in kvdb. Since the
// reclaim vector has already been advanced, this state cannot be
// detected, resulting in leakage of space. To prevent this, we could
// have persisted the GC state to support restartability. However, to
// keep GC light weight, we went with the approach of not persisting
// the transient GC state but lazily performing a consistency check on
// kvdb to detect dangling records. Online consistency check phase
// performs this checking. It checks every generation older than the
// reclaim vector snapshotted at bootstrap to see if it has any state
// left over in kvdb. If it finds dangling state, it marks the
// corresponding objects as dirty for pruning. This consistency check
// happens only once upon reboot. Once all generations lower than the
// reclaim vector snapshot are verified, this phase is a noop. Once
// again, to limit the overhead of this phase, it processes only a
// small batch of generations in each round of GC invocation.
//
// Finally, the underlying kvdb store persists state by writing to a
// log file upon flush. Thus, as we continue to flush to kvdb, the log
// file will keep growing. In addition, upon restart, this log file
// must be processed to reconstruct the kvdb state. To keep this log
// file from becoming large, we need to periodically compact kvdb.
import (
	"errors"
	"fmt"
	"time"

	"veyron/services/store/raw"

	"veyron2/storage"
	"veyron2/vlog"
)

var (
	// garbage collection (space reclamation) is invoked every
	// garbageCollectInterval.
	garbageCollectInterval = 3600 * time.Second

	// strictCheck when enabled performs strict checking of every
	// log record being deleted to confirm that it should be in
	// fact deleted.
	// TODO(hpucha): Support strictCheck in the presence
	// of Link log records.
	strictCheck = false

	// Every compactCount iterations of garbage collection, kvdb
	// is compacted.  This value has performance implications as
	// kvdb compaction is expensive.
	compactCount = 100

	// Batch size for the number of objects that are garbage
	// collected every gc iteration.  This value impacts the
	// amount of time gc is running. GC holds a write lock on the
	// data structures, blocking out all other operations in the
	// system while it is running.
	objBatchSize = 20

	// Batch size for the number of generations that are verified
	// every gc iteration.
	genBatchSize = 100

	// Errors.
	errBadMetadata = errors.New("bad metadata")
)

// objGCState tracks the per-object GC state.
// "version" is the most recent version of the object that can be
// pruned (and hence all older versions can be pruned as well).
//
// "pos" is used to compute the most recent version of the object
// among all the versions that can be pruned (version with the highest
// pos is the most recent). "version" of the object belongs to a
// generation. "pos" is the position of that generation in the local
// log.
type objGCState struct {
	version raw.Version
	pos     uint32
}

// objVersHist tracks all the versions of the object that need to be
// gc'ed when strictCheck is enabled.
type objVersHist struct {
	versions map[raw.Version]struct{}
}

// syncGC contains the metadata and state for the Sync GC thread.
type syncGC struct {
	// syncd is a pointer to the Syncd instance owning this GC.
	syncd *syncd

	// checkConsistency controls whether online consistency checks are run.
	checkConsistency bool

	// reclaimSnap is the snapshot of the reclaim vector at startup.
	reclaimSnap GenVector

	// pruneObjects holds the per-object state for garbage collection.
	pruneObjects map[storage.ID]*objGCState

	// verifyPruneMap holds the per-object version history to verify GC operations
	// on an object.  It is used when strictCheck is enabled.
	verifyPruneMap map[storage.ID]*objVersHist
}

// newGC creates a new syncGC instance attached to the given Syncd instance.
func newGC(syncd *syncd) *syncGC {
	g := &syncGC{
		syncd:            syncd,
		checkConsistency: true,
		reclaimSnap:      GenVector{},
		pruneObjects:     make(map[storage.ID]*objGCState),
	}

	if strictCheck {
		g.verifyPruneMap = make(map[storage.ID]*objVersHist)
	}

	// Take a snapshot (copy) of the reclaim vector at startup.
	for dev, gnum := range syncd.devtab.head.ReclaimVec {
		g.reclaimSnap[dev] = gnum
	}

	return g
}

// garbageCollect wakes up every garbageCollectInterval to check if it
// can reclaim space.
func (g *syncGC) garbageCollect() {
	gcIters := 0
	ticker := time.NewTicker(garbageCollectInterval)
	for {
		select {
		case <-g.syncd.closed:
			ticker.Stop()
			g.syncd.pending.Done()
			return
		case <-ticker.C:
			gcIters++
			if gcIters == compactCount {
				gcIters = 0
				g.doGC(true)
			} else {
				g.doGC(false)
			}
		}
	}
}

// doGC performs the actual garbage collection steps.
// If "compact" is true, also compact the Sync DBs.
func (g *syncGC) doGC(compact bool) {
	vlog.VI(1).Infof("doGC:: Started at %v", time.Now().UTC())

	g.syncd.lock.Lock()
	defer g.syncd.lock.Unlock()

	if err := g.onlineConsistencyCheck(); err != nil {
		vlog.Fatalf("onlineConsistencyCheck:: failed with err %v", err)
	}

	if err := g.reclaimSpace(); err != nil {
		vlog.Fatalf("reclaimSpace:: failed with err %v", err)
	}
	// TODO(hpucha): flush devtable state.

	if err := g.pruneObjectBatch(); err != nil {
		vlog.Fatalf("pruneObjectBatch:: failed with err %v", err)
	}
	// TODO(hpucha): flush log and dag state.

	if compact {
		if err := g.compactDB(); err != nil {
			vlog.Fatalf("compactDB:: failed with err %v", err)
		}
	}
}

// onlineConsistencyCheck checks if generations lower than the
// ReclaimVec (snapshotted at startup) are deleted from the log and
// dag data structures. It is needed to prevent space leaks when the
// system crashes while pruning a batch of objects. GC state is not
// aggressively persisted to make it efficient. Instead, upon reboot,
// onlineConsistencyCheck executes incrementally checking all the
// generations lower than the ReclaimVec snap to ensure that they are
// deleted. Each iteration of onlineConsistencyCheck checks a small
// batch of generations. Once all generations below the ReclaimVec
// snap are verified once, onlineConsistencyCheck is a noop.
func (g *syncGC) onlineConsistencyCheck() error {
	vlog.VI(1).Infof("onlineConsistencyCheck:: called with %v", g.checkConsistency)
	if !g.checkConsistency {
		return nil
	}

	vlog.VI(2).Infof("onlineConsistencyCheck:: reclaimSnap is %v", g.reclaimSnap)
	genCount := 0
	for dev, gen := range g.reclaimSnap {
		if gen == 0 {
			continue
		}
		for i := gen; i > 0; i-- {
			if genCount == genBatchSize {
				g.reclaimSnap[dev] = i
				return nil
			}
			if g.syncd.log.hasGenMetadata(dev, i) {
				if err := g.garbageCollectGeneration(dev, i); err != nil {
					return err
				}
			}

			genCount++
		}
		g.reclaimSnap[dev] = 0
	}

	// Done with all the generations of all devices. Consistency
	// check is no longer needed.
	g.checkConsistency = false
	vlog.VI(1).Infof("onlineConsistencyCheck:: exited with %v", g.checkConsistency)
	return nil
}

// garbageCollectGeneration garbage collects any existing log records
// for a particular generation.
func (g *syncGC) garbageCollectGeneration(devid DeviceID, gnum GenID) error {
	vlog.VI(2).Infof("garbageCollectGeneration:: processing generation %s:%d", devid, gnum)
	// Bootstrap generation for a device. Nothing to GC.
	if gnum == 0 {
		return nil
	}
	gen, err := g.syncd.log.getGenMetadata(devid, gnum)
	if err != nil {
		return err
	}
	if gen.Count <= 0 {
		return errBadMetadata
	}

	var count uint64
	// Check for log records for this generation.
	for l := LSN(0); l <= gen.MaxLSN; l++ {
		if !g.syncd.log.hasLogRec(devid, gnum, l) {
			continue
		}

		count++
		rec, err := g.syncd.log.getLogRec(devid, gnum, l)
		if err != nil {
			return err
		}

		if rec.RecType == LinkRec {
			// For a link log record, gc it right away.
			g.dagPruneCallBack(logRecKey(devid, gnum, l))
			continue
		}

		// Insert the object in this log record to the prune
		// map if needed.
		// If this object does not exist, create it.
		// If the object exists, update the object version to
		// prune at if the current gen is greater than the gen
		// in the prune map or if the gen is the same but the
		// current lsn is greater than the previous lsn.
		gcState, ok := g.pruneObjects[rec.ObjID]
		if !ok || gcState.pos <= gen.Pos {
			if !ok {
				gcState = &objGCState{}
				g.pruneObjects[rec.ObjID] = gcState
			}
			gcState.pos = gen.Pos
			gcState.version = rec.CurVers
			vlog.VI(2).Infof("Replacing for obj %v pos %d version %d",
				rec.ObjID, gcState.pos, gcState.version)
		}

		// When strictCheck is enabled, track object's version
		// history so that we can check against the versions
		// being deleted.
		if strictCheck {
			objHist, ok := g.verifyPruneMap[rec.ObjID]
			if !ok {
				objHist = &objVersHist{
					versions: make(map[raw.Version]struct{}),
				}
				g.verifyPruneMap[rec.ObjID] = objHist
			}
			// Add this version to the versions that need to be pruned.
			objHist.versions[rec.CurVers] = struct{}{}
		}
	}

	if count != gen.Count {
		return errors.New("incorrect number of log records")
	}

	return nil
}

// reclaimSpace performs periodic space reclamation by deleting
// generations known to all devices.
//
// Approach: For each device in the system, we compute its maximum
// generation known to all the other devices in the system. We then
// delete all log and dag records below this generation. This is a
// O(N^2) algorithm where N is the number of devices in the system.
func (g *syncGC) reclaimSpace() error {
	newReclaimVec, err := g.syncd.devtab.computeReclaimVector()
	if err != nil {
		return err
	}

	vlog.VI(1).Infof("reclaimSpace:: reclaimVectors: new %v old %v",
		newReclaimVec, g.syncd.devtab.head.ReclaimVec)
	// Clean up generations from reclaimVec+1 to newReclaimVec.
	for dev, high := range newReclaimVec {
		low := g.syncd.devtab.getOldestGen(dev)

		// Garbage collect from low+1 to high.
		for i := GenID(low + 1); i <= high; i++ {
			if err := g.garbageCollectGeneration(dev, i); err != nil {
				return err
			}
		}
	}

	// Update reclaimVec.
	g.syncd.devtab.head.ReclaimVec = newReclaimVec
	return nil
}

// pruneObjectBatch processes a batch of objects to be pruned from log
// and dag.
func (g *syncGC) pruneObjectBatch() error {
	vlog.VI(1).Infof("pruneObjectBatch:: Called at %v", time.Now().UTC())
	count := 0
	for obj, gcState := range g.pruneObjects {
		if count == objBatchSize {
			return nil
		}
		vlog.VI(1).Infof("pruneObjectBatch:: Pruning obj %v at version %v", obj, gcState.version)
		// Call dag prune on this object.
		if err := g.syncd.dag.prune(obj, gcState.version, g.dagPruneCallBack); err != nil {
			return err
		}

		if strictCheck {
			// Ensure that all but one version in the object version history are gc'ed.
			objHist, ok := g.verifyPruneMap[obj]
			if !ok {
				return fmt.Errorf("missing object in verification map %v", obj)
			}
			if len(objHist.versions) != 1 {
				return fmt.Errorf("leftover/no versions %d", len(objHist.versions))
			}
			for v, _ := range objHist.versions {
				if v != gcState.version {
					return fmt.Errorf("leftover version %d %v", v, obj)
				}
			}
		}

		delete(g.pruneObjects, obj)
		count++
	}

	return (g.syncd.dag.pruneDone())
}

// dagPruneCallBack deletes the log record associated with the dag
// node being pruned and updates the generation metadata for the
// generation that this log record belongs to.
func (g *syncGC) dagPruneCallBack(logKey string) error {
	dev, gnum, lsn, err := splitLogRecKey(logKey)
	if err != nil {
		return err
	}
	vlog.VI(2).Infof("dagPruneCallBack:: called for key %s (%s %d %d)", logKey, dev, gnum, lsn)

	// Check if the log record being deleted is correct as per GC state.
	oldestGen := g.syncd.devtab.getOldestGen(dev)
	if gnum > oldestGen {
		vlog.VI(2).Infof("gnum is %d oldest is %d", gnum, oldestGen)
		return errors.New("deleting incorrect log record")
	}

	if !g.syncd.log.hasLogRec(dev, gnum, lsn) {
		return errors.New("missing log record")
	}

	if strictCheck {
		rec, err := g.syncd.log.getLogRec(dev, gnum, lsn)
		if err != nil {
			return err
		}
		if rec.RecType == NodeRec {
			objHist, ok := g.verifyPruneMap[rec.ObjID]
			if !ok {
				return errors.New("obj not found in verifyMap")
			}
			_, found := objHist.versions[rec.CurVers]
			// If online consistency check is in progress, we
			// cannot strictly verify all the versions to be
			// deleted, and we ignore the failure to find a
			// version.
			if found {
				delete(objHist.versions, rec.CurVers)
			} else if !g.checkConsistency {
				return errors.New("verification failed")
			}
		}
	}

	if err := g.syncd.log.delLogRec(dev, gnum, lsn); err != nil {
		return err
	}

	// Update generation metadata.
	gen, err := g.syncd.log.getGenMetadata(dev, gnum)
	if err != nil {
		return err
	}
	if gen.Count <= 0 {
		return errBadMetadata
	}
	gen.Count--
	if gen.Count == 0 {
		if err := g.syncd.log.delGenMetadata(dev, gnum); err != nil {
			return err
		}
	} else {
		if err := g.syncd.log.putGenMetadata(dev, gnum, gen); err != nil {
			return err
		}
	}
	return nil
}

// compactDB compacts the underlying kvdb store for all data structures.
func (g *syncGC) compactDB() error {
	vlog.VI(1).Infof("compactDB:: Compacting DBs")
	if err := g.syncd.log.compact(); err != nil {
		return err
	}
	if err := g.syncd.devtab.compact(); err != nil {
		return err
	}
	if err := g.syncd.dag.compact(); err != nil {
		return err
	}
	return nil
}

// deleteDevice takes care of permanently deleting a device from its sync peers.
// TODO(hpucha): to be implemented.
func (g *syncGC) deleteDevice() {
}
