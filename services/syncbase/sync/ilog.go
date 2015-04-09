// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Package vsync provides veyron sync ILog utility functions.  ILog
// (Indexed Log) provides log functionality with indexing support.
// ILog stores log records that are locally generated or obtained over
// the network.  Indexing is needed since sync needs to selectively
// retrieve log records that belong to a particular device, syncroot
// and generation during synchronization.
//
// When a device receives a request to send log records, it first
// computes the missing generations between itself and the incoming
// request on a per-syncroot basis. It then sends all the log records
// belonging to each missing generation.  A device that receives log
// records over the network replays all the records received from
// another device in a single batch. Each replayed log record adds a
// new version to the dag of the object contained in the log
// record. At the end of replaying all the log records, conflict
// detection and resolution is carried out for all the objects learned
// during this iteration. Conflict detection and resolution is carried
// out after a batch of log records are replayed, instead of
// incrementally after each record is replayed, to avoid repeating
// conflict resolution already performed by other devices.
//
// New log records are created when objects in the local store are
// created/updated. Local log records are also replayed to keep the
// per-object dags consistent with the local store state.
//
// Note that syncgroups are mainly tracked between syncd/store and the
// client. Sync-related metadata (log records, generations and
// generation vectors) is unaware of syncgroups, and uses syncroots
// instead. Each syncgroup contains a root object and a unique root
// object ID. In case of peer syncgroups, all the peer syncgroups
// contain the same root object ID.  Sync translates any given
// syncgroup to a syncroot rooted at this root object ID and syncs all
// the syncroots the device is part of.  Thus, although a device may
// be part of more than 1 peer syncgroup, at the sync level, a single
// syncroot is synced on behalf of all the peer syncgroups.
//
// Implementation notes: ILog records are stored in a persistent K/V
// database in the current implementation.  ILog db consists of 3
// tables:
// ** records: table consists of all the log records indexed
// by deviceid:srid:genid:lsn referring to
//         the device that creates the log record
//         the root oid of the syncroot to which the log record belongs
//         the generation on the syncroot the log record is part of
//         the sequence number in that generation
// Note that lsn in each generation starts from 0 and genid starts from 1.
// ** gens: table consists of the generation metadata for each
// generation, and is indexed by deviceid:srid:genid.
// ** head: table consists of the log header.
import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"v.io/x/lib/vlog"
)

var (
	errNoUpdates  = errors.New("no new local updates")
	errInvalidLog = errors.New("invalid log db")
)

// curLocalGen contains metadata re. the current local generation for a SyncRoot.
type curLocalGen struct {
	CurGenNum GenId  // generation id for a SyncRoot's current generation.
	CurSeqNum SeqNum // log sequence number for a SyncRoot's current generation.
}

// iLogHeader contains the log header metadata.
type iLogHeader struct {
	CurSRGens map[ObjId]*curLocalGen // local generation info per SyncRoot.
	Curorder  uint32                 // position in log for the next generation.
}

// genMetadata contains the metadata for a generation.
type genMetadata struct {
	// All generations stored in the log are ordered wrt each
	// other and this order needs to be preserved.
	// Position of this generation in the log.
	Pos uint32

	// Number of log records in this generation still stored in the log.
	// This count is used during garbage collection.
	Count uint64

	// Maximum SeqNum that was part of this generation.
	// This is useful during garbage collection to track any unclaimed log records.
	MaxSeqNum SeqNum
}

// iLog contains the metadata for the ILog db.
type iLog struct {
	fname string // file pathname.
	db    *kvdb  // underlying k/v db.

	// Key:deviceid:srid:genid:lsn Value:LogRecord. Pointer to
	// the "records" table in the kvdb. Contains log records.
	records *kvtable

	// Key:deviceid:srid:genid Value:genMetadata. Pointer to the
	// "gens" table in the kvdb. Contains generation metadata for
	// each generation.
	gens *kvtable

	// Key:"Head" Value:iLogHeader. Pointer to the "header" table
	// in the kvdb. Contains logheader.
	header *kvtable

	head *iLogHeader // log head cached in memory.

	s *syncd // pointer to the sync daemon object.
}

// openILog opens or creates a ILog for the given filename.
func openILog(filename string, sin *syncd) (*iLog, error) {
	ilog := &iLog{
		fname: filename,
		head:  &iLogHeader{},
		s:     sin,
	}
	// Open the file and create it if it does not exist.
	// Also initialize the kvdb and its three collections.
	db, tbls, err := kvdbOpen(filename, []string{"records", "gens", "header"})
	if err != nil {
		return nil, err
	}

	ilog.db = db
	ilog.records = tbls[0]
	ilog.gens = tbls[1]
	ilog.header = tbls[2]

	// If header already exists in db, read it back from db.
	if ilog.hasHead() {
		if err := ilog.getHead(); err != nil {
			ilog.db.close() // this also closes the tables.
			return nil, err
		}
	} else {
		ilog.head.CurSRGens = make(map[ObjId]*curLocalGen)
	}

	return ilog, nil
}

// close closes the ILog and invalidate its struct.
func (l *iLog) close() error {
	if l.db == nil {
		return errInvalidLog
	}
	// Flush the dirty data.
	if err := l.flush(); err != nil {
		return err
	}

	l.db.close() // this also closes the tables.

	*l = iLog{} // zero out the ILog struct.
	return nil
}

// flush flushes the ILog db to disk.
func (l *iLog) flush() error {
	if l.db == nil {
		return errInvalidLog
	}
	// Set the head from memory before flushing.
	if err := l.putHead(); err != nil {
		return err
	}

	l.db.flush()

	return nil
}

// initSyncRoot initializes the log header for this SyncRoot.
func (l *iLog) initSyncRoot(srid ObjId) error {
	if l.db == nil {
		return errInvalidLog
	}
	if _, ok := l.head.CurSRGens[srid]; ok {
		return fmt.Errorf("syncroot already exists %v", srid)
	}
	// First generation to be created is generation 1. A
	// generation of 0 represents no updates on the device.
	l.head.CurSRGens[srid] = &curLocalGen{CurGenNum: 1}
	return nil
}

// delSyncRoot deletes the log header for this SyncRoot.
func (l *iLog) delSyncRoot(srid ObjId) error {
	if l.db == nil {
		return errInvalidLog
	}
	if _, ok := l.head.CurSRGens[srid]; !ok {
		return fmt.Errorf("syncroot doesn't exist %v", srid)
	}

	delete(l.head.CurSRGens, srid)
	return nil
}

// putHead puts the log head into the ILog db.
func (l *iLog) putHead() error {
	return l.header.set("Head", l.head)
}

// getHead gets the log head from the ILog db.
func (l *iLog) getHead() error {
	if l.head == nil {
		return errors.New("nil log header")
	}
	if err := l.header.get("Head", l.head); err != nil {
		return err
	}
	// When we put an empty map in kvdb, we get back a "nil" map
	// (due to VOM encoding/decoding). To keep putHead/getHead
	// symmetrical, we add this additional initialization.
	if l.head.CurSRGens == nil {
		l.head.CurSRGens = make(map[ObjId]*curLocalGen)
	}
	return nil
}

// hasHead returns true if the ILog db has a log head.
func (l *iLog) hasHead() bool {
	return l.header.hasKey("Head")
}

// logRecKey creates a key for a log record.
func logRecKey(devid DeviceId, srid ObjId, gnum GenId, lsn SeqNum) string {
	return fmt.Sprintf("%s:%s:%d:%d", devid, srid.String(), uint64(gnum), uint64(lsn))
}

// strToGenId converts a string to GenId.
func strToGenId(genIDStr string) (GenId, error) {
	id, err := strconv.ParseUint(genIDStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return GenId(id), nil
}

// strToObjId converts a string to ObjId.
func strToObjId(objIDStr string) (ObjId, error) {
	return ObjId(objIDStr), nil
}

// splitLogRecKey splits a : separated logrec key into its components.
func splitLogRecKey(key string) (DeviceId, ObjId, GenId, SeqNum, error) {
	args := strings.Split(key, ":")
	if len(args) != 4 {
		return "", NoObjId, 0, 0, fmt.Errorf("bad logrec key %s", key)
	}
	srid, err := strToObjId(args[1])
	if err != nil {
		return "", NoObjId, 0, 0, err
	}
	gnum, err := strToGenId(args[2])
	if err != nil {
		return "", NoObjId, 0, 0, err
	}
	lsn, err := strconv.ParseUint(args[3], 10, 64)
	if err != nil {
		return "", NoObjId, 0, 0, err
	}
	return DeviceId(args[0]), srid, gnum, SeqNum(lsn), nil
}

// putLogRec puts the log record into the ILog db.
func (l *iLog) putLogRec(rec *LogRec) (string, error) {
	if l.db == nil {
		return "", errInvalidLog
	}
	key := logRecKey(rec.DevId, rec.SyncRootId, rec.GenNum, rec.SeqNum)
	return key, l.records.set(key, rec)
}

// getLogRec gets the log record from the ILog db.
func (l *iLog) getLogRec(devid DeviceId, srid ObjId, gnum GenId, lsn SeqNum) (*LogRec, error) {
	if l.db == nil {
		return nil, errInvalidLog
	}
	key := logRecKey(devid, srid, gnum, lsn)
	var rec LogRec
	if err := l.records.get(key, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// hasLogRec returns true if the ILog db has a log record matching (devid, srid, gnum, lsn).
func (l *iLog) hasLogRec(devid DeviceId, srid ObjId, gnum GenId, lsn SeqNum) bool {
	if l.db == nil {
		return false
	}
	key := logRecKey(devid, srid, gnum, lsn)
	return l.records.hasKey(key)
}

// delLogRec deletes the log record matching (devid, srid, gnum, lsn) from the ILog db.
func (l *iLog) delLogRec(devid DeviceId, srid ObjId, gnum GenId, lsn SeqNum) error {
	if l.db == nil {
		return errInvalidLog
	}
	key := logRecKey(devid, srid, gnum, lsn)
	return l.records.del(key)
}

// generationKey creates a key for a generation.
func generationKey(devid DeviceId, srid ObjId, gnum GenId) string {
	return fmt.Sprintf("%s:%s:%d", devid, srid.String(), gnum)
}

// splitGenerationKey splits a : separated generation key into its components.
func splitGenerationKey(key string) (DeviceId, ObjId, GenId, error) {
	args := strings.Split(key, ":")
	if len(args) != 3 {
		return "", NoObjId, 0, fmt.Errorf("bad generation key %s", key)
	}
	srid, err := strToObjId(args[1])
	if err != nil {
		return "", NoObjId, 0, err
	}
	gnum, err := strToGenId(args[2])
	if err != nil {
		return "", NoObjId, 0, err
	}
	return DeviceId(args[0]), srid, gnum, nil
}

// putGenMetadata puts the metadata of the generation (devid, srid, gnum) into the ILog db.
func (l *iLog) putGenMetadata(devid DeviceId, srid ObjId, gnum GenId, val *genMetadata) error {
	key := generationKey(devid, srid, gnum)
	return l.gens.set(key, val)
}

// getGenMetadata gets the metadata of the generation (devid, srid, gnum) from the ILog db.
func (l *iLog) getGenMetadata(devid DeviceId, srid ObjId, gnum GenId) (*genMetadata, error) {
	if l.db == nil {
		return nil, errInvalidLog
	}
	key := generationKey(devid, srid, gnum)
	var val genMetadata
	if err := l.gens.get(key, &val); err != nil {
		return nil, err
	}
	return &val, nil
}

// hasGenMetadata returns true if the ILog db has the generation (devid, srid, gnum).
func (l *iLog) hasGenMetadata(devid DeviceId, srid ObjId, gnum GenId) bool {
	key := generationKey(devid, srid, gnum)
	return l.gens.hasKey(key)
}

// delGenMetadata deletes the generation (devid, srid, gnum) metadata from the ILog db.
func (l *iLog) delGenMetadata(devid DeviceId, srid ObjId, gnum GenId) error {
	if l.db == nil {
		return errInvalidLog
	}
	key := generationKey(devid, srid, gnum)
	return l.gens.del(key)
}

// getLocalGenInfo gets the local generation info for the given syncroot.
func (l *iLog) getLocalGenInfo(srid ObjId) (*curLocalGen, error) {
	gen, ok := l.head.CurSRGens[srid]
	if !ok {
		return nil, fmt.Errorf("no gen info found for srid %s", srid.String())
	}
	return gen, nil
}

// createLocalLogRec creates a new local log record of type NodeRec.
func (l *iLog) createLocalLogRec(obj ObjId, vers Version,
	par []Version, val *LogValue, srid ObjId) (*LogRec, error) {
	gen, err := l.getLocalGenInfo(srid)
	if err != nil {
		return nil, err
	}
	rec := &LogRec{
		DevId:      l.s.id,
		SyncRootId: srid,
		GenNum:     gen.CurGenNum,
		SeqNum:     gen.CurSeqNum,
		RecType:    NodeRec,

		ObjId:   obj,
		CurVers: vers,
		Parents: par,
		Value:   *val,
	}

	// Increment the SeqNum for the local log.
	gen.CurSeqNum++

	return rec, nil
}

// createLocalLinkLogRec creates a new local log record of type LinkRec.
func (l *iLog) createLocalLinkLogRec(obj ObjId, vers, par Version, srid ObjId) (*LogRec, error) {
	gen, err := l.getLocalGenInfo(srid)
	if err != nil {
		return nil, err
	}
	rec := &LogRec{
		DevId:      l.s.id,
		SyncRootId: srid,
		GenNum:     gen.CurGenNum,
		SeqNum:     gen.CurSeqNum,
		RecType:    LinkRec,

		ObjId:   obj,
		CurVers: vers,
		Parents: []Version{par},
		Value:   LogValue{},
	}

	// Increment the SeqNum for the local log.
	gen.CurSeqNum++

	return rec, nil
}

// createRemoteGeneration adds a new remote generation.
func (l *iLog) createRemoteGeneration(dev DeviceId, srid ObjId, gnum GenId, gen *genMetadata) error {
	if l.db == nil {
		return errInvalidLog
	}

	if gen.Count != uint64(gen.MaxSeqNum+1) {
		return errors.New("mismatch in count and lsn")
	}

	vlog.VI(2).Infof("createRemoteGeneration:: dev %s srid %s gen %d %v", dev, srid.String(), gnum, gen)

	gen.Pos = l.head.Curorder
	l.head.Curorder++

	return l.putGenMetadata(dev, srid, gnum, gen)
}

// createLocalGeneration creates a new local generation.
func (l *iLog) createLocalGeneration(srid ObjId) (GenId, error) {
	if l.db == nil {
		return 0, errInvalidLog
	}

	g, err := l.getLocalGenInfo(srid)
	if err != nil {
		return 0, err
	}

	gnum := g.CurGenNum

	// If there are no updates, there will be no new generation.
	if g.CurSeqNum == 0 {
		return gnum - 1, errNoUpdates
	}

	// Add the current generation to the db.
	val := &genMetadata{
		Pos:       l.head.Curorder,
		Count:     uint64(g.CurSeqNum),
		MaxSeqNum: g.CurSeqNum - 1,
	}

	err = l.putGenMetadata(l.s.id, srid, gnum, val)

	vlog.VI(2).Infof("createLocalGeneration:: created srid %s gen %d %v", srid.String(), gnum, val)
	// Move to the next generation irrespective of err.
	l.head.Curorder++
	g.CurGenNum++
	g.CurSeqNum = 0

	return gnum, err
}

// processWatchRecord processes new object versions obtained from the local store.
func (l *iLog) processWatchRecord(objID ObjId, vers, parent Version, val *LogValue, srid ObjId) error {
	if l.db == nil {
		return errInvalidLog
	}

	vlog.VI(2).Infof("processWatchRecord:: adding for object %v %v (srid %v)", objID, vers, srid)

	if !srid.IsValid() {
		return errors.New("invalid syncroot id")
	}

	// Filter out the echo from the watcher. When syncd puts mutations into store,
	// it hears back these mutations once again on the watch stream. We need to
	// filter out these echoes and only process brand new watch changes.
	if vers != NoVersion {
		// Check if the object's vers already exists in the DAG.
		if l.s.dag.hasNode(objID, vers) {
			return nil
		}

		// When we successfully join a SyncGroup, Store
		// creates an empty directory with object ID as the
		// rootObjId of the SyncGroup joined.  We do not care
		// about this version of the directory. We will start
		// accepting local mutations on this object only after
		// we hear about it remotely first.
		//
		// NOTE: Since this new directory is protected from
		// any local updates via an Permissions until after the first
		// sync, waiting to hear remotely first works without
		// dropping any updates. However, we cache in the
		// "priv" table its version in the Store so that we
		// can correctly specify prior version when we put
		// mutations into the store.
		if _, err := l.s.dag.getHead(objID); err != nil && objID == srid {
			priv := &privNode{ /*Mutation: &val.Mutation, */ SyncTime: val.SyncTime, TxId: val.TxId, TxCount: val.TxCount}
			return l.s.dag.setPrivNode(objID, priv)
		}
	} else {
		// Check if the parent version has a deleted
		// descendant already in the DAG.
		if l.s.dag.hasDeletedDescendant(objID, parent) {
			return nil
		}
	}

	var pars []Version
	if parent != NoVersion {
		pars = []Version{parent}
	}

	// If the current version is a deletion, generate a new version number.
	if val.Delete {
		if vers != NoVersion {
			return fmt.Errorf("deleted vers is %v", vers)
		}
		vers = NewVersion()
		//val.Mutation.Version = vers
	}

	// Create a log record from Watch's Change Record.
	rec, err := l.createLocalLogRec(objID, vers, pars, val, srid)
	if err != nil {
		return err
	}

	// Insert the new log record into the log.
	logKey, err := l.putLogRec(rec)
	if err != nil {
		return err
	}

	// Insert the new log record into dag.
	if err = l.s.dag.addNode(rec.ObjId, rec.CurVers, false, val.Delete, rec.Parents, logKey, val.TxId); err != nil {
		return err
	}

	// Move the head.
	return l.s.dag.moveHead(rec.ObjId, rec.CurVers)
}

// dump writes to the log file information on ILog internals.
func (l *iLog) dump() {
	if l.db == nil {
		return
	}

	vlog.VI(1).Infof("DUMP: ILog: #SR %d, cur %d", len(l.head.CurSRGens), l.head.Curorder)
	for sr, gen := range l.head.CurSRGens {
		vlog.VI(1).Infof("DUMP: ILog: SR %v: gen %v, lsn %v", sr, gen.CurGenNum, gen.CurSeqNum)
	}

	// Find for each (devid, srid) pair its lowest and highest generation numbers.
	type genInfo struct{ min, max GenId }
	devs := make(map[DeviceId]map[ObjId]*genInfo)

	l.gens.keyIter(func(genKey string) {
		devid, srid, gnum, err := splitGenerationKey(genKey)
		if err != nil {
			return
		}

		srids, ok := devs[devid]
		if !ok {
			srids = make(map[ObjId]*genInfo)
			devs[devid] = srids
		}

		info, ok := srids[srid]
		if ok {
			if gnum > info.max {
				info.max = gnum
			}
			if gnum < info.min {
				info.min = gnum
			}
		} else {
			srids[srid] = &genInfo{min: gnum, max: gnum}
		}
	})

	// For each (devid, srid) pair dump its generation info in order from
	// min to max generation numbers, inclusive.
	for devid, srids := range devs {
		for srid, info := range srids {
			for gnum := info.min; gnum <= info.max; gnum++ {
				meta, err := l.getGenMetadata(devid, srid, gnum)
				if err == nil {
					vlog.VI(1).Infof(
						"DUMP: ILog: gen (%v, %v, %v): pos %v, count %v, maxlsn %v",
						devid, srid, gnum, meta.Pos, meta.Count, meta.MaxSeqNum)
				} else {
					vlog.VI(1).Infof("DUMP: ILog: gen (%v, %v, %v): missing generation",
						devid, srid, gnum)
				}
			}
		}
	}
}
