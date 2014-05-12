package vsync

// Package vsync provides veyron sync ILog utility functions.  ILog
// (Indexed Log) provides log functionality with indexing support.
// ILog stores log records that are locally generated or obtained over
// the network.  Indexing is needed since sync needs to selectively
// retrieve log records that belong to a particular device and
// generation during synchronization.
//
// When a device receives a request to send log records, it first
// computes the missing generations between itself and the incoming
// request. It then sends all the log records belonging to each
// missing generation.  A device that receives log records over the
// network replays all the records received from another device in a
// single batch. Each replayed log record adds a new version to the
// dag of the object contained in the log record. At the end of
// replaying all the log records, conflict detection and resolution is
// carried out for all the objects learned during this
// iteration. Conflict detection and resolution is carried out after a
// batch of log records are replayed, instead of incrementally after
// each record is replayed, to avoid repeating conflict resolution
// already performed by other devices.
//
// New log records are created when objects in the local store are
// created/updated. Local log records are also replayed to keep the
// per-object dags consistent with the local store state.
//
// Implementation notes: ILog records are stored in a persistent K/V
// database in the current implementation.  ILog db consists of 3
// tables:
// ** records: table consists of all the log records indexed
// by deviceid:genid:lsn referring to the device that creates the log
// record, the generation on the device the log record is part of, and
// its sequence number in that generation.  Note that lsn in each
// generation starts from 0 and genid starts from 1.
// ** gens: table consists of the generation metadata for each
// generation, and is indexed by deviceid:genid.
// ** head: table consists of the log header.
import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"veyron2/storage"
)

var (
	errNoUpdates  = errors.New("no new local updates")
	errInvalidLog = errors.New("invalid log db")
)

// iLogHeader contains the log header metadata.
type iLogHeader struct {
	Curgen   GenID  // generation id for a device's current generation.
	Curlsn   LSN    // log sequence number for a device's current generation.
	Curorder uint32 // position in log for the next generation.
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

	// Maximum LSN that was part of this generation.
	// This is useful during garbage collection to track any unclaimed log records.
	MaxLSN LSN
}

// iLog contains the metadata for the ILog db.
type iLog struct {
	fname string // file pathname.
	db    *kvdb  // underlying k/v db.

	// Key:deviceid-genid-lsn Value:LogRecord
	records *kvtable // pointer to the "records" table in the kvdb. Contains log records.

	// Key:deviceid-genid Value:genMetadata
	gens *kvtable // pointer to the "gens" table in the kvdb. Contains generation metadata for each generation.

	// Key:"Head" Value:iLogHeader
	header *kvtable // pointer to the "header" table in the kvdb. Contains logheader.

	head *iLogHeader // log head cached in memory.

	s *syncd // pointer to the sync daemon object.
}

// openILog opens or creates a ILog for the given filename.
func openILog(filename string, sin *syncd) (*iLog, error) {
	ilog := &iLog{
		fname: filename,
		head:  nil,
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

	// Initialize the log header.
	// First generation to be created is generation 1. A generation of 0
	// represents no updates on the device.
	ilog.head = &iLogHeader{
		Curgen: 1,
	}
	// If header already exists in db, read it back from db.
	if ilog.hasHead() {
		if err := ilog.getHead(); err != nil {
			ilog.db.close() // this also closes the tables.
			return nil, err
		}
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

// compact compacts the file associated with kvdb.
func (l *iLog) compact() error {
	if l.db == nil {
		return errInvalidLog
	}
	db, tbls, err := l.db.compact(l.fname, []string{"records", "gens", "header"})
	if err != nil {
		return err
	}
	l.db = db
	l.records = tbls[0]
	l.gens = tbls[1]
	l.header = tbls[2]
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
	err := l.header.get("Head", l.head)
	return err
}

// hasHead returns true if the ILog db has a log head.
func (l *iLog) hasHead() bool {
	return l.header.hasKey("Head")
}

// logRecKey creates a key for a log record.
func logRecKey(devid DeviceID, gnum GenID, lsn LSN) string {
	return fmt.Sprintf("%s:%d:%d", devid, uint64(gnum), uint64(lsn))
}

// splitLogRecKey splits a : separated logrec key into its components.
func splitLogRecKey(key string) (DeviceID, GenID, LSN, error) {
	args := strings.Split(key, ":")
	if len(args) != 3 {
		return "", 0, 0, fmt.Errorf("bad logrec key %s", key)
	}
	gnum, _ := strconv.ParseUint(args[1], 10, 64)
	lsn, _ := strconv.ParseUint(args[2], 10, 64)
	return DeviceID(args[0]), GenID(gnum), LSN(lsn), nil
}

// putLogRec puts the log record into the ILog db.
func (l *iLog) putLogRec(rec *LogRec) (string, error) {
	if l.db == nil {
		return "", errInvalidLog
	}
	key := logRecKey(rec.DevID, rec.GNum, rec.LSN)
	return key, l.records.set(key, rec)
}

// getLogRec gets the log record from the ILog db.
func (l *iLog) getLogRec(devid DeviceID, gnum GenID, lsn LSN) (*LogRec, error) {
	if l.db == nil {
		return nil, errInvalidLog
	}
	key := logRecKey(devid, gnum, lsn)
	var rec LogRec
	if err := l.records.get(key, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// hasLogRec returns true if the ILog db has a log record matching (devid, gnum, lsn).
func (l *iLog) hasLogRec(devid DeviceID, gnum GenID, lsn LSN) bool {
	if l.db == nil {
		return false
	}
	key := logRecKey(devid, gnum, lsn)
	return l.records.hasKey(key)
}

// delLogRec deletes the log record matching (devid, gnum, lsn) from the ILog db.
func (l *iLog) delLogRec(devid DeviceID, gnum GenID, lsn LSN) error {
	if l.db == nil {
		return errInvalidLog
	}
	key := logRecKey(devid, gnum, lsn)
	return l.records.del(key)
}

// generationKey creates a key for a generation.
func generationKey(devid DeviceID, gnum GenID) string {
	return fmt.Sprintf("%s:%d", devid, gnum)
}

// splitGenerationKey splits a : separated logrec key into its components.
func splitGenerationKey(key string) (DeviceID, GenID, error) {
	args := strings.Split(key, ":")
	if len(args) != 2 {
		return "", 0, fmt.Errorf("bad generation key %s", key)
	}
	gnum, _ := strconv.ParseUint(args[1], 10, 64)
	return DeviceID(args[0]), GenID(gnum), nil
}

// putGenMetadata puts the metadata of the generation (devid, gnum) into the ILog db.
func (l *iLog) putGenMetadata(devid DeviceID, gnum GenID, val *genMetadata) error {
	key := generationKey(devid, gnum)
	return l.gens.set(key, val)
}

// getGenMetadata gets the metadata of the generation (devid, gnum) from the ILog db.
func (l *iLog) getGenMetadata(devid DeviceID, gnum GenID) (*genMetadata, error) {
	if l.db == nil {
		return nil, errInvalidLog
	}
	key := generationKey(devid, gnum)
	var val genMetadata
	if err := l.gens.get(key, &val); err != nil {
		return nil, err
	}
	return &val, nil
}

// hasGenMetadata returns true if the ILog db has the generation (devid, gnum).
func (l *iLog) hasGenMetadata(devid DeviceID, gnum GenID) bool {
	key := generationKey(devid, gnum)
	return l.gens.hasKey(key)
}

// delGenMetadata deletes the generation (devid, gnum) metadata from the ILog db.
func (l *iLog) delGenMetadata(devid DeviceID, gnum GenID) error {
	if l.db == nil {
		return errInvalidLog
	}
	key := generationKey(devid, gnum)
	return l.gens.del(key)
}

// createLocalLogRec creates a new local log record.
func (l *iLog) createLocalLogRec(obj storage.ID, vers storage.Version, par []storage.Version, val *LogValue) (*LogRec, error) {
	rec := &LogRec{
		DevID: l.s.id,
		GNum:  l.head.Curgen,
		LSN:   l.head.Curlsn,

		ObjID:   obj,
		CurVers: vers,
		Parents: par,
		Value:   *val,
	}

	// Increment the LSN for the local log.
	l.head.Curlsn++

	return rec, nil
}

// createRemoteGeneration adds a new remote generation.
func (l *iLog) createRemoteGeneration(dev DeviceID, gnum GenID, gen *genMetadata) error {
	if l.db == nil {
		return errInvalidLog
	}

	if gen.Count != uint64(gen.MaxLSN+1) {
		return errors.New("mismatch in count and lsn")
	}

	gen.Pos = l.head.Curorder
	l.head.Curorder++

	return l.putGenMetadata(dev, gnum, gen)
}

// createLocalGeneration creates a new local generation.
// createLocalGeneration is currently called when there is an incoming GetDeltas request.
func (l *iLog) createLocalGeneration() (GenID, error) {
	if l.db == nil {
		return 0, errInvalidLog
	}

	g := l.head.Curgen

	// If there are no updates, there will be no new generation.
	if l.head.Curlsn == 0 {
		return g - 1, errNoUpdates
	}

	// Add the current generation to the db.
	val := &genMetadata{
		Pos:    l.head.Curorder,
		Count:  uint64(l.head.Curlsn),
		MaxLSN: l.head.Curlsn - 1,
	}
	err := l.putGenMetadata(l.s.id, g, val)

	// Move to the next generation irrespective of err.
	l.head.Curorder++
	l.head.Curgen++
	l.head.Curlsn = 0

	return g, err
}

// processWatchRecord processes new object versions obtained from the local store.
func (l *iLog) processWatchRecord(objID storage.ID, vers storage.Version, par []storage.Version, val *LogValue) error {
	if l.db == nil {
		return errInvalidLog
	}
	// Check if the object version already exists in the DAG. if so return.
	if l.s.dag.hasNode(objID, vers) {
		return nil
	}

	// Create a log record from Watch's Change Record.
	rec, err := l.createLocalLogRec(objID, vers, par, val)
	if err != nil {
		return err
	}

	// Insert the new log record into the log.
	logKey, err := l.putLogRec(rec)
	if err != nil {
		return err
	}

	// Insert the new log record into dag.
	if err = l.s.dag.addNode(rec.ObjID, rec.CurVers, false, rec.Parents, logKey); err != nil {
		return err
	}

	// Move the head.
	if err := l.s.dag.moveHead(rec.ObjID, rec.CurVers); err != nil {
		return err
	}

	return nil
}

// dumpILog dumps the ILog data structure.
func (l *iLog) dumpILog() {
	fmt.Println("In-memory Header")
	fmt.Println("Current generation: ", l.head.Curgen, l.head.Curlsn, " Current order: ",
		l.head.Curorder)

	fmt.Println("================================================")

	fmt.Println("In-DB Header")
	head := &iLogHeader{}
	if err := l.header.get("Head", head); err != nil {
		fmt.Println("Couldn't access in DB header")
	} else {
		fmt.Println("Current generation: ", head.Curgen, head.Curlsn, " Current order: ", head.Curorder)
	}
	fmt.Println("================================================")
}

// fillFakeWatchRecords fills fake log and dag state (testing only).
// TODO(hpucha): remove
func (l *iLog) fillFakeWatchRecords() {
	const num = 10
	var parvers []storage.Version
	id := storage.NewID()
	for i := int(0); i < num; i++ {
		// Create a local log record.
		curvers := storage.Version(i)
		if err := l.processWatchRecord(id, curvers, parvers, &LogValue{}); err != nil {
			return
		}
		parvers = []storage.Version{curvers}
	}
}
