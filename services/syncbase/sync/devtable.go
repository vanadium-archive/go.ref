// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Package vsync provides veyron sync DevTable utility functions.
// DevTable is indexed by the device id and stores device level
// information needed by sync.  Main component of a device's info are
// a set of generation vectors: one per SyncRoot. Generation vector
// is the version vector for a device's view of a SyncRoot,
// representing all the different generations (from different devices)
// seen by that device for that SyncRoot. A generation represents a
// collection of updates that originated on a device during an
// interval of time. It serves as a checkpoint when communicating with
// other devices. Generations do not overlap and all updates belong to
// a generation.
//
// Synchronization for a given SyncRoot between two devices A and B
// uses generation vectors as follows:
//                 A                              B
//                                     <== B's generation vector
// diff(A's generation vector, B's generation vector)
// log records of missing generations ==>
// cache B's generation vector (for space reclamation)
//
// Implementation notes: DevTable is stored in a persistent K/V
// database in the current implementation.  Generation vector is
// implemented as a map of (Device ID -> Generation ID), one entry for
// every known device.  If the generation vector contains an entry
// (Device ID -> Generation ID), it implies that the device has
// learned of all the generations until and including Generation
// ID. Generation IDs start from 1.  A generation ID of 0 is a
// reserved bootstrap value, and indicates the device has no updates.
import (
	"errors"
	"fmt"
	"sort"
	"time"

	"v.io/x/lib/vlog"
)

var (
	errInvalidDTab = errors.New("invalid devtable db")
)

// devInfo is the information stored per device.
type devInfo struct {
	Vectors map[ObjId]GenVector // device generation vectors.
	Ts      time.Time           // last communication time stamp.
}

// devTableHeader contains the header metadata.
type devTableHeader struct {
	Resmark []byte // resume marker for watch.
	// Generation vector for space reclamation. All generations
	// less than this generation vector are deleted from storage.
	ReclaimVec GenVector
}

// devTable contains the metadata for the device table db.
type devTable struct {
	fname   string   // file pathname.
	db      *kvdb    // underlying K/V DB.
	devices *kvtable // pointer to the "devices" table in the kvdb. Contains device info.

	// Key:"Head" Value:devTableHeader
	header *kvtable        // pointer to the "header" table in the kvdb. Contains device table header.
	head   *devTableHeader // devTable head cached in memory.

	s *syncd // pointer to the sync daemon object.
}

// genOrder represents a generation along with its position in the log.
type genOrder struct {
	devID DeviceId
	srID  ObjId
	genID GenId
	order uint32
}

// byOrder is used to sort the genOrder array.
type byOrder []*genOrder

func (a byOrder) Len() int {
	return len(a)
}

func (a byOrder) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a byOrder) Less(i, j int) bool {
	return a[i].order < a[j].order
}

// openDevTable opens or creates a devTable for the given filename.
func openDevTable(filename string, sin *syncd) (*devTable, error) {
	dtab := &devTable{
		fname: filename,
		s:     sin,
	}
	// Open the file and create it if it does not exist.
	// Also initialize the kvdb and its collection.
	db, tbls, err := kvdbOpen(filename, []string{"devices", "header"})
	if err != nil {
		return nil, err
	}

	dtab.db = db
	dtab.devices = tbls[0]
	dtab.header = tbls[1]

	// Initialize the devTable header.
	dtab.head = &devTableHeader{
		ReclaimVec: GenVector{
			dtab.s.id: 0,
		},
	}
	// If header already exists in db, read it back from db.
	if dtab.hasHead() {
		if err := dtab.getHead(); err != nil {
			dtab.db.close() // this also closes the tables.
			return nil, err
		}
	}

	return dtab, nil
}

// close closes the devTable and invalidates its struct.
func (dt *devTable) close() error {
	if dt.db == nil {
		return errInvalidDTab
	}
	// Flush the dirty data.
	if err := dt.flush(); err != nil {
		return err
	}
	dt.db.close() // this also closes the tables.

	*dt = devTable{} // zero out the devTable struct.
	return nil
}

// flush flushes the devTable db to storage.
func (dt *devTable) flush() error {
	if dt.db == nil {
		return errInvalidDTab
	}
	// Set the head from memory before flushing.
	if err := dt.putHead(); err != nil {
		return err
	}
	dt.db.flush()
	return nil
}

// initSyncRoot initializes the local generation vector for this SyncRoot.
func (dt *devTable) initSyncRoot(srid ObjId) error {
	if dt.db == nil {
		return errInvalidDTab
	}
	if _, err := dt.getGenVec(dt.s.id, srid); err == nil {
		return fmt.Errorf("syncroot already exists %v", srid)
	}
	return dt.putGenVec(dt.s.id, srid, GenVector{dt.s.id: 0})
}

// delSyncRoot deletes the generation vector for this SyncRoot.
func (dt *devTable) delSyncRoot(srid ObjId) error {
	if dt.db == nil {
		return errInvalidDTab
	}
	info, err := dt.getDevInfo(dt.s.id)
	if err != nil {
		return fmt.Errorf("dev doesn't exists %v", dt.s.id)
	}
	if _, ok := info.Vectors[srid]; !ok {
		return fmt.Errorf("syncroot doesn't exist %v", srid)
	}
	delete(info.Vectors, srid)
	if len(info.Vectors) == 0 {
		return dt.delDevInfo(dt.s.id)
	}
	return dt.putDevInfo(dt.s.id, info)
}

// putHead puts the devTable head into the devTable db.
func (dt *devTable) putHead() error {
	return dt.header.set("Head", dt.head)
}

// getHead gets the devTable head from the devTable db.
func (dt *devTable) getHead() error {
	if dt.head == nil {
		return errors.New("nil devTable header")
	}
	return dt.header.get("Head", dt.head)
}

// hasHead returns true if the devTable db has a devTable head.
func (dt *devTable) hasHead() bool {
	return dt.header.hasKey("Head")
}

// putDevInfo puts a devInfo struct in the devTable db.
func (dt *devTable) putDevInfo(devid DeviceId, info *devInfo) error {
	if dt.db == nil {
		return errInvalidDTab
	}
	return dt.devices.set(string(devid), info)
}

// getDevInfo gets a devInfo struct from the devTable db.
func (dt *devTable) getDevInfo(devid DeviceId) (*devInfo, error) {
	if dt.db == nil {
		return nil, errInvalidDTab
	}
	var info devInfo
	if err := dt.devices.get(string(devid), &info); err != nil {
		return nil, err
	}
	if info.Vectors == nil {
		return nil, errors.New("nil genvectors")
	}
	return &info, nil
}

// hasDevInfo returns true if the device (devid) has any devInfo in the devTable db.
func (dt *devTable) hasDevInfo(devid DeviceId) bool {
	if dt.db == nil {
		return false
	}
	return dt.devices.hasKey(string(devid))
}

// delDevInfo deletes devInfo struct in the devTable db.
func (dt *devTable) delDevInfo(devid DeviceId) error {
	if dt.db == nil {
		return errInvalidDTab
	}
	return dt.devices.del(string(devid))
}

// putGenVec puts a generation vector in the devTable db.
func (dt *devTable) putGenVec(devid DeviceId, srid ObjId, v GenVector) error {
	if dt.db == nil {
		return errInvalidDTab
	}
	var info *devInfo
	if dt.hasDevInfo(devid) {
		var err error
		if info, err = dt.getDevInfo(devid); err != nil {
			return err
		}
	} else {
		info = &devInfo{
			Vectors: make(map[ObjId]GenVector),
		}
	}
	info.Vectors[srid] = v
	info.Ts = time.Now().UTC()
	return dt.putDevInfo(devid, info)
}

// getGenVec gets a generation vector from the devTable db.
func (dt *devTable) getGenVec(devid DeviceId, srid ObjId) (GenVector, error) {
	if dt.db == nil {
		return nil, errInvalidDTab
	}
	info, err := dt.getDevInfo(devid)
	if err != nil {
		return nil, err
	}
	v, ok := info.Vectors[srid]
	if !ok {
		return nil, fmt.Errorf("srid %s doesn't exist", srid.String())
	}
	return v, nil
}

// populateGenOrderEntry populates a genOrder entry.
func (dt *devTable) populateGenOrderEntry(e *genOrder, id DeviceId, srid ObjId, gnum GenId) error {
	e.devID = id
	e.srID = srid
	e.genID = gnum

	o, err := dt.s.log.getGenMetadata(id, srid, gnum)
	if err != nil {
		return err
	}
	e.order = o.Pos
	return nil
}

// updateGeneration updates a single generation (upID, upGen) in a device's generation vector for SyncRoot srID.
func (dt *devTable) updateGeneration(key DeviceId, srID ObjId, upID DeviceId, upGen GenId) error {
	if dt.db == nil {
		return errInvalidDTab
	}
	info, err := dt.getDevInfo(key)
	if err != nil {
		return err
	}

	v, ok := info.Vectors[srID]
	if !ok {
		return fmt.Errorf("srid %s doesn't exist", srID.String())
	}
	v[upID] = upGen
	return dt.putDevInfo(key, info)
}

// updateLocalGenVector updates local generation vector based on the remote generation vector.
func (dt *devTable) updateLocalGenVector(local, remote GenVector) error {
	if dt.db == nil {
		return errInvalidDTab
	}
	if local == nil || remote == nil {
		return errors.New("invalid input args to function")
	}
	for rid, rgen := range remote {
		lgen, ok := local[rid]
		if !ok || lgen < rgen {
			local[rid] = rgen
		}
	}
	return nil
}

// diffGenVectors diffs generation vectors for a given SyncRoot belonging
// to src and dest and returns the generations known to src and not known to
// dest. In addition, sync needs to maintain the order in which device
// generations are created/received. Hence, when two generation
// vectors are diffed, the differing generations are returned in a
// sorted order based on their position in the src's log.  genOrder
// array consists of every generation that is missing between src and
// dest sorted using its position in the src's log.
// Example: Generation vector for device A (src) AVec = {A:10, B:5, C:1}
//          Generation vector for device B (dest) BVec = {A:5, B:10, D:2}
// Missing generations in unsorted order: {A:6, A:7, A:8, A:9, A:10, C:1}
//
// TODO(hpucha): Revisit for the case of a lot of generations to
// send back (say during bootstrap).
func (dt *devTable) diffGenVectors(srcVec, destVec GenVector, srid ObjId) ([]*genOrder, error) {
	if dt.db == nil {
		return nil, errInvalidDTab
	}

	// Create an array for the generations that need to be returned.
	var gens []*genOrder

	// Compute missing generations for devices that are in destination and source vector.
	for devid, genid := range destVec {
		srcGenId, ok := srcVec[devid]
		// Skip since src doesn't know of this device.
		if !ok {
			continue
		}
		// Need to include all generations in the interval [genid+1, srcGenId],
		// genid+1 and srcGenId inclusive.
		// Check against reclaimVec to see if required generations are already GCed.
		// Starting gen is then max(oldGen, genid+1)
		startGen := genid + 1
		oldGen := dt.getOldestGen(devid) + 1
		if startGen < oldGen {
			vlog.VI(1).Infof("diffGenVectors:: Adjusting starting generations from %d to %d",
				startGen, oldGen)
			startGen = oldGen
		}
		for i := startGen; i <= srcGenId; i++ {
			// Populate the genorder entry.
			var entry genOrder
			if err := dt.populateGenOrderEntry(&entry, devid, srid, i); err != nil {
				return nil, err
			}
			gens = append(gens, &entry)
		}
	}
	// Compute missing generations for devices not in destination vector but in source vector.
	for devid, genid := range srcVec {
		// Add devices destination does not know about.
		if _, ok := destVec[devid]; !ok {
			// Bootstrap generation to oldest available.
			destGenId := dt.getOldestGen(devid) + 1
			// Need to include all generations in the interval [destGenId, genid],
			// destGenId and genid inclusive.
			for i := destGenId; i <= genid; i++ {
				// Populate the genorder entry.
				var entry genOrder
				if err := dt.populateGenOrderEntry(&entry, devid, srid, i); err != nil {
					return nil, err
				}
				gens = append(gens, &entry)
			}
		}
	}

	// Sort generations in log order.
	sort.Sort(byOrder(gens))
	return gens, nil
}

// getOldestGen returns the most recent gc'ed generation for the device "dev".
func (dt *devTable) getOldestGen(dev DeviceId) GenId {
	return dt.head.ReclaimVec[dev]
}

// // computeReclaimVector computes a generation vector such that the
// // generations less than or equal to those in the vector can be
// // garbage collected. Caller holds a lock on s.lock.
// //
// // Approach: For each device in the system, we compute its maximum
// // generation known to all the other devices in the system. This is a
// // O(N^2) algorithm where N is the number of devices in the system. N
// // is assumed to be small, of the order of hundreds of devices.
// func (dt *devTable) computeReclaimVector() (GenVector, error) {
// 	// Get local generation vector to create the set of devices in
// 	// the system. Local generation vector is a good bootstrap
// 	// device set since it contains all the devices whose log
// 	// records were ever stored locally.
// 	devSet, err := dt.getGenVec(dt.s.id)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	newReclaimVec := GenVector{}
// 	for devid := range devSet {
// 		if !dt.hasDevInfo(devid) {
// 			// This node knows of devid, but hasn't yet
// 			// contacted the device. Do not garbage
// 			// collect any further. For instance, when
// 			// node A learns of node C's generations from
// 			// node B, node A may not have an entry for
// 			// node C yet, but node C will be part of its
// 			// devSet.
// 			for dev := range devSet {
// 				newReclaimVec[dev] = dt.getOldestGen(dev)
// 			}
// 			return newReclaimVec, nil
// 		}
//
// 		vec, err := dt.getGenVec(devid)
// 		if err != nil {
// 			return nil, err
// 		}
// 		for dev := range devSet {
// 			gen1, ok := vec[dev]
// 			// Device "devid" does not know about device "dev".
// 			if !ok {
// 				newReclaimVec[dev] = dt.getOldestGen(dev)
// 				continue
// 			}
// 			gen2, ok := newReclaimVec[dev]
// 			if !ok || (gen1 < gen2) {
// 				newReclaimVec[dev] = gen1
// 			}
// 		}
// 	}
// 	return newReclaimVec, nil
// }
//
// // addDevice adds a newly learned device to the devTable state.
// func (dt *devTable) addDevice(newDev DeviceId) error {
// 	// Create an entry in the device table for the new device.
// 	vector := GenVector{
// 		newDev: 0,
// 	}
// 	if err := dt.putGenVec(newDev, vector); err != nil {
// 		return err
// 	}
//
// 	// Update local generation vector with the new device.
// 	local, err := dt.getDevInfo(dt.s.id)
// 	if err != nil {
// 		return err
// 	}
// 	if err := dt.updateLocalGenVector(local.Vector, vector); err != nil {
// 		return err
// 	}
// 	if err := dt.putDevInfo(dt.s.id, local); err != nil {
// 		return err
// 	}
// 	return nil
// }
//
// // updateReclaimVec updates the reclaim vector to track gc'ed generations.
// func (dt *devTable) updateReclaimVec(minGens GenVector) error {
// 	for dev, min := range minGens {
// 		gen, ok := dt.head.ReclaimVec[dev]
// 		if !ok {
// 			if min < 1 {
// 				vlog.Errorf("updateReclaimVec:: Received bad generation %s %d",
// 					dev, min)
// 				dt.head.ReclaimVec[dev] = 0
// 			} else {
// 				dt.head.ReclaimVec[dev] = min - 1
// 			}
// 			continue
// 		}
//
// 		// We obtained a generation that is already reclaimed.
// 		if min <= gen {
// 			return errors.New("requested gen smaller than GC'ed gen")
// 		}
// 	}
// 	return nil
// }

// getDeviceIds returns the IDs of all devices that are involved in synchronization.
func (dt *devTable) getDeviceIds() ([]DeviceId, error) {
	if dt.db == nil {
		return nil, errInvalidDTab
	}

	devIDs := make([]DeviceId, 0)
	dt.devices.keyIter(func(devStr string) {
		devIDs = append(devIDs, DeviceId(devStr))
	})

	return devIDs, nil
}

// dump writes to the log file information on all device table entries.
func (dt *devTable) dump() {
	if dt.db == nil {
		return
	}

	vlog.VI(1).Infof("DUMP: Dev: self %v: resmark %v, reclaim %v",
		dt.s.id, dt.head.Resmark, dt.head.ReclaimVec)

	dt.devices.keyIter(func(devStr string) {
		info, err := dt.getDevInfo(DeviceId(devStr))
		if err != nil {
			return
		}

		vlog.VI(1).Infof("DUMP: Dev: %s: #SR %d, time %v", devStr, len(info.Vectors), info.Ts)
		for sr, vec := range info.Vectors {
			vlog.VI(1).Infof("DUMP: Dev: %s: SR %v, vec %v", devStr, sr, vec)
		}
	})
}
