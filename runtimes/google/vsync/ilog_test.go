package vsync

// Tests for the Veyron Sync ILog component.
import (
	"os"
	"reflect"
	"testing"

	"veyron2/storage"
)

// TestILogStore tests creating a backing file for ILog.
func TestILogStore(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new ILog file %s, err %v", logfile, err)
	}

	fsize := getFileSize(logfile)
	if fsize < 0 {
		t.Errorf("Log file %s not created", logfile)
	}

	if err := log.flush(); err != nil {
		t.Errorf("Cannot flush ILog file %s, err %v", logfile, err)
	}

	oldfsize := fsize
	fsize = getFileSize(logfile)
	if fsize <= oldfsize {
		t.Errorf("Log file %s not flushed", logfile)
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close ILog file %s, err %v", logfile)
	}

	oldfsize = getFileSize(logfile)

	log, err = openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot re-open existing log file %s, err %v", logfile)
	}

	fsize = getFileSize(logfile)
	if fsize != oldfsize {
		t.Errorf("Log file %s size changed across re-open (%d %d)", logfile, fsize, oldfsize)
	}

	if err := log.flush(); err != nil {
		t.Errorf("Cannot flush ILog file %s, err %v", logfile)
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close ILog file %s, err %v", logfile)
	}
}

// TestInvalidLog tests log methods on an invalid (closed) log ptr.
func TestInvalidLog(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new ILog file %s, err %v", logfile, err)
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close ILog file %s, err %v", logfile, err)
	}

	err = log.close()
	if err == nil || err != errInvalidLog {
		t.Errorf("Close did not fail on a closed log: %v", err)
	}

	err = log.flush()
	if err == nil || err != errInvalidLog {
		t.Errorf("Flush did not fail on a closed log: %v", err)
	}

	err = log.compact()
	if err == nil || err != errInvalidLog {
		t.Errorf("Compact did not fail on a closed log: %v", err)
	}

	_, err = log.putLogRec(&LogRec{})
	if err == nil || err != errInvalidLog {
		t.Errorf("PutLogRec did not fail on a closed log: %v", err)
	}

	var devid DeviceID = "VeyronPhone"
	var gnum GenID = 1
	var lsn LSN

	_, err = log.getLogRec(devid, gnum, lsn)
	if err == nil || err != errInvalidLog {
		t.Errorf("GetLogRec did not fail on a closed log: %v", err)
	}

	if log.hasLogRec(devid, gnum, lsn) {
		if err == nil || err != errInvalidLog {
			t.Errorf("HasLogRec did not fail on a closed log: %v", err)
		}
	}

	err = log.delLogRec(devid, gnum, lsn)
	if err == nil || err != errInvalidLog {
		t.Errorf("DelLogRec did not fail on a closed log: %v", err)
	}

	_, err = log.getGenMetadata(devid, gnum)
	if err == nil || err != errInvalidLog {
		t.Errorf("GetGenMetadata did not fail on a closed log: %v", err)
	}

	err = log.delGenMetadata(devid, gnum)
	if err == nil || err != errInvalidLog {
		t.Errorf("DelGenMetadata did not fail on a closed log: %v", err)
	}

	err = log.createRemoteGeneration(devid, gnum, &genMetadata{})
	if err == nil || err != errInvalidLog {
		t.Errorf("CreateRemoteGeneration did not fail on a closed log: %v", err)
	}

	_, err = log.createLocalGeneration()
	if err == nil || err != errInvalidLog {
		t.Errorf("CreateLocalGeneration did not fail on a closed log: %v", err)
	}

	err = log.processWatchRecord(storage.NewID(), 2, []storage.Version{0, 1}, &LogValue{})
	if err == nil || err != errInvalidLog {
		t.Errorf("ProcessWatchRecord did not fail on a closed log: %v", err)
	}
}

// TestPutGetLogHeader tests setting and getting log header across log open/close/reopen.
func TestPutGetLogHeader(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	// In memory head should be initialized.
	if log.head.Curgen != 1 || log.head.Curlsn != 0 || log.head.Curorder != 0 {
		t.Errorf("First time log create should reset header")
	}

	// No head should be there in db.
	if err = log.getHead(); err == nil {
		t.Errorf("getHead() found non-existent head in log file %s, err %v", logfile, err)
	}

	if log.hasHead() {
		t.Errorf("hasHead() found non-existent head in log file %s", logfile)
	}

	log.head = &iLogHeader{
		Curgen:   10,
		Curlsn:   100,
		Curorder: 1000,
	}

	if err := log.putHead(); err != nil {
		t.Errorf("Cannot put head %v in log file %s, err %v", log.head, logfile, err)
	}

	// Reset values.
	log.head.Curgen = 0
	log.head.Curlsn = 0
	log.head.Curorder = 0

	for i := 0; i < 2; i++ {
		if err := log.getHead(); err != nil {
			t.Fatalf("getHead() can not find head (i=%d) in log file %s, err %v", i, logfile, err)
		}

		if !log.hasHead() {
			t.Errorf("hasHead() can not find head (i=%d) in log file %s", i, logfile)
		}

		if log.head.Curgen != 10 && log.head.Curlsn != 100 && log.head.Curorder != 1000 {
			t.Errorf("Data mismatch for head (i=%d) in log file %s: %v",
				i, logfile, log.head)
		}

		if i == 0 {
			if err := log.close(); err != nil {
				t.Errorf("Cannot close log file %s, err %v", logfile, err)
			}
			log, err = openILog(logfile, nil)
			if err != nil {
				t.Fatalf("Cannot re-open log file %s, err %v", logfile, err)
			}
		}
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestPersistLogHeader tests that log header is automatically persisted across log open/close/reopen.
func TestPersistLogHeader(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	// In memory head should be initialized.
	if log.head.Curgen != 1 || log.head.Curlsn != 0 || log.head.Curorder != 0 {
		t.Errorf("First time log create should reset header")
	}

	log.head = &iLogHeader{
		Curgen:   10,
		Curlsn:   100,
		Curorder: 1000,
	}

	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}

	log, err = openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	// In memory head should be initialized from db.
	if log.head.Curgen != 10 && log.head.Curlsn != 100 && log.head.Curorder != 1000 {
		t.Errorf("Data mismatch for head in log file %s: %v", logfile, log.head)
	}

	log.head = &iLogHeader{
		Curgen:   1000,
		Curlsn:   10,
		Curorder: 100,
	}

	if err := log.flush(); err != nil {
		t.Errorf("Cannot flush ILog file %s, err %v", logfile, err)
	}

	// Reset values.
	log.head.Curgen = 0
	log.head.Curlsn = 0
	log.head.Curorder = 0

	if err := log.getHead(); err != nil {
		t.Fatalf("getHead() can not find head in log file %s, err %v", logfile, err)
	}

	// In memory head should be initialized from db.
	if log.head.Curgen != 1000 && log.head.Curlsn != 10 && log.head.Curorder != 100 {
		t.Errorf("Data mismatch for head in log file %s: %v", logfile, log.head)
	}

	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestPutGetLogRec tests setting and getting a log record across log open/close/reopen.
func TestPutGetLogRec(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	var devid DeviceID = "VeyronTab"
	var gnum GenID = 100
	var lsn LSN = 1000

	rec, err := log.getLogRec(devid, gnum, lsn)
	if err == nil || rec != nil {
		t.Errorf("GetLogRec() found non-existent object %s:%d:%d in log file %s: %v, err %v",
			devid, gnum, lsn, logfile, rec, err)
	}

	if log.hasLogRec(devid, gnum, lsn) {
		t.Errorf("HasLogRec() found non-existent object %s:%d:%d in log file %s",
			devid, gnum, lsn, logfile)
	}

	objID := storage.NewID()
	rec = &LogRec{
		DevID:   devid,
		GNum:    gnum,
		LSN:     lsn,
		ObjID:   objID,
		CurVers: 2,
		Parents: []storage.Version{0, 1},
		Value:   LogValue{},
	}

	if _, err := log.putLogRec(rec); err != nil {
		t.Errorf("Cannot put object %s:%d:%d (%v) in log file %s, err %v", devid, gnum, lsn, rec, logfile, err)
	}

	for i := 0; i < 2; i++ {
		curRec, err := log.getLogRec(devid, gnum, lsn)
		if err != nil || curRec == nil {
			t.Fatalf("GetLogRec() can not find object %s:%d:%d (i=%d) in log file %s, err %v",
				devid, gnum, lsn, i, logfile, err)
		}

		if !log.hasLogRec(devid, gnum, lsn) {
			t.Errorf("HasLogRec() can not find object %s:%d:%d (i=%d) in log file %s",
				devid, gnum, lsn, i, logfile)
		}

		if !reflect.DeepEqual(rec, curRec) {
			t.Errorf("Data mismatch for object %s:%d:%d (i=%d) in log file %s: %v instead of %v",
				devid, gnum, lsn, i, logfile, curRec, rec)
		}

		if i == 0 {
			if err := log.close(); err != nil {
				t.Errorf("Cannot close log file %s, err %v", logfile, err)
			}
			log, err = openILog(logfile, nil)
			if err != nil {
				t.Fatalf("Cannot re-open log file %s, err %v", logfile, err)
			}
		}
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestDelLogRec tests deleting a log record across log open/close/reopen.
func TestDelLogRec(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	var devid DeviceID = "VeyronTab"
	var gnum GenID = 100
	var lsn LSN = 1000

	objID := storage.NewID()
	rec := &LogRec{
		DevID:   devid,
		GNum:    gnum,
		LSN:     lsn,
		ObjID:   objID,
		CurVers: 2,
		Parents: []storage.Version{0, 1},
		Value:   LogValue{},
	}

	if _, err := log.putLogRec(rec); err != nil {
		t.Errorf("Cannot put object %s:%d:%d (%v) in log file %s, err %v", devid, gnum, lsn, rec, logfile, err)
	}

	curRec, err := log.getLogRec(devid, gnum, lsn)
	if err != nil || curRec == nil {
		t.Fatalf("GetLogRec() can not find object %s:%d:%d in log file %s, err %v",
			devid, gnum, lsn, logfile, err)
	}

	if err := log.delLogRec(devid, gnum, lsn); err != nil {
		t.Fatalf("DelLogRec() can not delete object %s:%d:%d in log file %s, err %v",
			devid, gnum, lsn, logfile, err)
	}

	for i := 0; i < 2; i++ {
		curRec, err = log.getLogRec(devid, gnum, lsn)
		if err == nil || curRec != nil {
			t.Fatalf("GetLogRec() finds deleted object %s:%d:%d (i=%d) in log file %s, err %v",
				devid, gnum, lsn, i, logfile, err)
		}

		if log.hasLogRec(devid, gnum, lsn) {
			t.Errorf("HasLogRec() finds deleted object %s:%d:%d (i=%d) in log file %s",
				devid, gnum, lsn, i, logfile)
		}

		if i == 0 {
			if err := log.close(); err != nil {
				t.Errorf("Cannot close log file %s, err %v", logfile, err)
			}
			log, err = openILog(logfile, nil)
			if err != nil {
				t.Fatalf("Cannot re-open log file %s, err %v", logfile, err)
			}
		}
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}

}

// TestPutGetGenMetadata tests setting and getting generation metadata across log open/close/reopen.
func TestPutGetGenMetadata(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	var devid DeviceID = "VeyronTab"
	var gnum GenID = 100

	val, err := log.getGenMetadata(devid, gnum)
	if err == nil || val != nil {
		t.Errorf("GetGenMetadata() found non-existent object %s:%d in log file %s: %v, err %v",
			devid, gnum, logfile, val, err)
	}

	if log.hasGenMetadata(devid, gnum) {
		t.Errorf("hasGenMetadata() found non-existent object %s:%d in log file %s",
			devid, gnum, logfile)
	}

	val = &genMetadata{Pos: 40, Count: 100, MaxLSN: 99}
	if err := log.putGenMetadata(devid, gnum, val); err != nil {
		t.Errorf("Cannot put object %s:%d in log file %s, err %v", devid, gnum, logfile, err)
	}

	for i := 0; i < 2; i++ {
		curVal, err := log.getGenMetadata(devid, gnum)
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%d (i=%d) in log file %s, err %v",
				devid, gnum, i, logfile, err)
		}

		if !log.hasGenMetadata(devid, gnum) {
			t.Errorf("hasGenMetadata() can not find object %s:%d (i=%d) in log file %s",
				devid, gnum, i, logfile)
		}

		if !reflect.DeepEqual(val, curVal) {
			t.Errorf("Data mismatch for object %s:%d (i=%d) in log file %s: %v instead of %v",
				devid, gnum, i, logfile, curVal, val)
		}

		if i == 0 {
			if err := log.close(); err != nil {
				t.Errorf("Cannot close log file %s, err %v", logfile, err)
			}
			log, err = openILog(logfile, nil)
			if err != nil {
				t.Fatalf("Cannot re-open log file %s, err %v", logfile, err)
			}
		}
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestDelGenMetadata tests deleting generation metadata across log open/close/reopen.
func TestDelGenMetadata(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	var devid DeviceID = "VeyronTab"
	var gnum GenID = 100

	val := &genMetadata{Pos: 40, Count: 100, MaxLSN: 99}
	if err := log.putGenMetadata(devid, gnum, val); err != nil {
		t.Errorf("Cannot put object %s:%d in log file %s, err %v", devid, gnum, logfile, err)
	}

	curVal, err := log.getGenMetadata(devid, gnum)
	if err != nil || curVal == nil {
		t.Fatalf("GetGenMetadata() can not find object %s:%d in log file %s, err %v",
			devid, gnum, logfile, err)
	}

	if err := log.delGenMetadata(devid, gnum); err != nil {
		t.Fatalf("DelGenMetadata() can not delete object %s:%d in log file %s, err %v",
			devid, gnum, logfile, err)
	}

	for i := 0; i < 2; i++ {
		curVal, err := log.getGenMetadata(devid, gnum)
		if err == nil || curVal != nil {
			t.Fatalf("GetGenMetadata() finds deleted object %s:%d (i=%d) in log file %s, err %v",
				devid, gnum, i, logfile, err)
		}

		if log.hasGenMetadata(devid, gnum) {
			t.Errorf("hasGenMetadata() finds deleted object %s:%d (i=%d) in log file %s",
				devid, gnum, i, logfile)
		}

		if i == 0 {
			if err := log.close(); err != nil {
				t.Errorf("Cannot close log file %s, err %v", logfile, err)
			}
			log, err = openILog(logfile, nil)
			if err != nil {
				t.Fatalf("Cannot re-open log file %s, err %v", logfile, err)
			}
		}
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestPersistLogState tests that generation metadata and record state
// is persisted across log open/close/reopen.
func TestPersistLogState(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	var devid DeviceID = "VeyronTab"

	// Add several generations.
	for i := uint32(0); i < 10; i++ {
		val := &genMetadata{Pos: i}
		if err := log.putGenMetadata(devid, GenID(i+10), val); err != nil {
			t.Errorf("Cannot put object %s:%d in log file %s, err %v", devid, i, logfile, err)
		}
	}
	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
	log, err = openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot re-open log file %s, err %v", logfile, err)
	}
	for i := uint32(0); i < 10; i++ {
		curVal, err := log.getGenMetadata(devid, GenID(i+10))
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%d in log file %s, err %v",
				devid, i, logfile, err)
		}
		if curVal.Pos != i {
			t.Errorf("Data mismatch for object %s:%d in log file %s: %v",
				devid, i, logfile, curVal)
		}
		// Should safely overwrite the same keys.
		curVal.Pos = i + 10
		if err := log.putGenMetadata(devid, GenID(i+10), curVal); err != nil {
			t.Errorf("Cannot put object %s:%d in log file %s, err %v", devid, i, logfile, err)
		}
	}
	for i := uint32(0); i < 10; i++ {
		curVal, err := log.getGenMetadata(devid, GenID(i+10))
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%d in log file %s, err %v",
				devid, i, logfile, err)
		}
		if curVal.Pos != (i + 10) {
			t.Errorf("Data mismatch for object %s:%d in log file %s: %v, err %v",
				devid, i, logfile, curVal, err)
		}
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// fillFakeLogRecords fills fake log records for testing purposes.
func (l *iLog) fillFakeLogRecords() {
	const num = 10
	var parvers []storage.Version
	id := storage.NewID()
	for i := int(0); i < num; i++ {
		// Create a local log record.
		curvers := storage.Version(i)
		rec, err := l.createLocalLogRec(id, curvers, parvers, &LogValue{})
		if err != nil {
			return
		}
		// Insert the new log record into the log.
		_, err = l.putLogRec(rec)
		if err != nil {
			return
		}
		parvers = []storage.Version{curvers}
	}
}

// TestCreateGeneration tests that local log records and local
// generations are created uniquely and remote generations are
// correctly inserted in log order.
func TestCreateGeneration(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	s := &syncd{id: "VeyronTab"}
	log, err := openILog(logfile, s)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	if g, err := log.createLocalGeneration(); err != errNoUpdates {
		t.Errorf("Should not find local updates gen %d with error %v", g, err)
	}

	const num = 10
	var parvers []storage.Version
	id := storage.NewID()
	for i := int(0); i < num; i++ {
		// Create a local log record.
		curvers := storage.Version(i)
		rec, err := log.createLocalLogRec(id, curvers, parvers, &LogValue{})
		if err != nil {
			t.Fatalf("Cannot create local log rec ObjID: %v Current: %s Parents: %v Error: %v",
				id, curvers, parvers, err)
		}

		temprec := &LogRec{
			DevID:   log.s.id,
			GNum:    GenID(1),
			LSN:     LSN(i),
			ObjID:   id,
			CurVers: curvers,
			Parents: parvers,
			Value:   LogValue{},
		}
		// Verify that the log record has the right values.
		if !reflect.DeepEqual(rec, temprec) {
			t.Errorf("Data mismtach in log record %v instead of %v", rec, temprec)
		}

		// Insert the new log record into the log.
		_, err = log.putLogRec(rec)
		if err != nil {
			t.Errorf("Cannot put log record:: failed with err %v", err)
		}

		parvers = []storage.Version{curvers}
	}

	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}

	log, err = openILog(logfile, s)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	if log.head.Curgen != 1 || log.head.Curlsn != num {
		t.Errorf("Data mismatch in log header %v", log.head)
	}

	g, err := log.createLocalGeneration()
	if g != 1 || err != nil {
		t.Errorf("Could not create local generation gen %d with error %v", g, err)
	}
	curVal, err := log.getGenMetadata(log.s.id, g)
	if err != nil || curVal == nil {
		t.Fatalf("GetGenMetadata() can not find object %s:%d in log file %s, err %v",
			log.s.id, g, logfile, err)
	}
	expVal := &genMetadata{Pos: 0, Count: 10, MaxLSN: 9}
	if !reflect.DeepEqual(expVal, curVal) {
		t.Errorf("Data mismatch for object %s:%d in log file %s: %v instead of %v",
			log.s.id, g, logfile, curVal, expVal)
	}
	if log.head.Curgen != 2 || log.head.Curlsn != 0 || log.head.Curorder != 1 {
		t.Errorf("Data mismatch in log header %v", log.head)
	}

	if g, err := log.createLocalGeneration(); err != errNoUpdates {
		t.Errorf("Should not find local updates gen %d with error %v", g, err)
	}

	// Populate one more generation.
	log.fillFakeLogRecords()

	g, err = log.createLocalGeneration()
	if g != 2 || err != nil {
		t.Errorf("Could not create local generation gen %d with error %v", g, err)
	}

	// Create a remote generation.
	expGen := &genMetadata{Count: 1, MaxLSN: 50}
	if err = log.createRemoteGeneration("VeyronPhone", 1, expGen); err == nil {
		t.Errorf("Remote generation create should have failed", g, err)
	}
	expGen.MaxLSN = 0
	if err = log.createRemoteGeneration("VeyronPhone", 1, expGen); err != nil {
		t.Errorf("createRemoteGeneration failed with err %v", err)
	}
	if expGen.Pos != 2 {
		t.Errorf("createRemoteGeneration created incorrect log order %d", expGen.Pos)
	}

	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}

	// Reopen the log and ensure that all log records for generations exist.
	// Also ensure that generation metadata is accurate.
	log, err = openILog(logfile, s)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	for g := GenID(1); g < 3; g++ {
		// Check all log records.
		for i := LSN(0); i < 10; i++ {
			curRec, err := log.getLogRec(log.s.id, g, i)
			if err != nil || curRec == nil {
				t.Fatalf("GetLogRec() can not find object %s:%d:%d in log file %s, err %v",
					log.s.id, g, i, logfile, err)
			}
		}
		// Check generation metadata.
		curVal, err := log.getGenMetadata(log.s.id, g)
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%d in log file %s, err %v",
				log.s.id, g, logfile, err)
		}
		expVal.Pos = uint32(g - 1)
		if !reflect.DeepEqual(expVal, curVal) {
			t.Errorf("Data mismatch for object %s:%d in log file %s: %v instead of %v",
				log.s.id, g, logfile, curVal, expVal)
		}
	}

	// Check remote generation metadata.
	curVal, err = log.getGenMetadata("VeyronPhone", 1)
	if err != nil || curVal == nil {
		t.Fatalf("GetGenMetadata() can not find object in log file %s, err %v",
			logfile, err)
	}
	if !reflect.DeepEqual(expGen, curVal) {
		t.Errorf("Data mismatch for object in log file %s: %v instead of %v",
			logfile, curVal, expGen)
	}
	if log.head.Curgen != 3 || log.head.Curlsn != 0 || log.head.Curorder != 3 {
		t.Errorf("Data mismatch in log header %v", log.head)
	}
	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestProcessWatchRecord tests that local updates are correctly handled.
// Commands are in file testdata/local-init-00.sync.
func TestProcessWatchRecord(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	dagfile := getFileName()
	defer os.Remove(dagfile)

	var err error
	s := &syncd{id: "VeyronTab"}
	if s.dag, err = openDAG(dagfile); err != nil {
		t.Fatalf("Cannot open new dag file %s, err %v", logfile, err)
	}
	log, err := openILog(logfile, s)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	if _, err = logReplayCommands(log, "local-init-00.sync"); err != nil {
		t.Error(err)
	}

	if log.head.Curgen != 1 || log.head.Curlsn != 3 || log.head.Curorder != 0 {
		t.Errorf("Data mismatch in log header %v", log.head)
	}

	objid, err := strToObjID("12345")
	if err != nil {
		t.Errorf("Could not create objid %v", err)
	}

	// Check all log records.
	for i := LSN(0); i < 3; i++ {
		curRec, err := log.getLogRec(log.s.id, GenID(1), i)
		if err != nil || curRec == nil {
			t.Fatalf("GetLogRec() can not find object %s:%d:%d in log file %s, err %v",
				log.s.id, 0, i, logfile, err)
		}

		if curRec.ObjID != objid {
			t.Errorf("Data mismatch in log record %v", curRec)
		}
	}

	// Verify DAG state.
	if head, err := log.s.dag.getHead(objid); err != nil || head != 2 {
		t.Errorf("Invalid object %d head in DAG %s, err %v", objid, head, err)
	}

	s.dag.flush()
	s.dag.close()
	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestILogCompact tests compacting of ilog's kvdb file.
func TestILogCompact(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	s := &syncd{id: "VeyronTab"}
	log, err := openILog(logfile, s)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	// Put some data in "records" table.
	log.fillFakeLogRecords()
	if err := log.flush(); err != nil {
		t.Errorf("Cannot flush ILog file %s, err %v", logfile, err)
	}

	// Put some data in "gens" table.
	for i := uint32(0); i < 10; i++ {
		val := &genMetadata{Pos: i, Count: uint64(i + 100)}
		if err := log.putGenMetadata(s.id, GenID(i+10), val); err != nil {
			t.Errorf("Cannot put object %s:%d in log file %s, err %v", s.id, i, logfile, err)
		}
		// Flush immediately to let the kvdb file grow.
		if err := log.flush(); err != nil {
			t.Errorf("Cannot flush ILog file %s, err %v", logfile, err)
		}
	}

	// Put some data in "header" table.
	log.head = &iLogHeader{
		Curgen:   1000,
		Curlsn:   10,
		Curorder: 100,
	}
	if err := log.flush(); err != nil {
		t.Errorf("Cannot flush ILog file %s, err %v", logfile, err)
	}

	// Get size before compaction.
	oldSize := getFileSize(logfile)
	if oldSize < 0 {
		t.Fatalf("Log file %s not created", logfile)
	}

	if err := log.compact(); err != nil {
		t.Errorf("Cannot compact ILog file %s, err %v", logfile, err)
	}

	// Verify size of kvdb file is reduced.
	size := getFileSize(logfile)
	if size < 0 {
		t.Fatalf("ILog file %s not created", logfile)
	}
	if size > oldSize {
		t.Fatalf("ILog file %s not compacted", logfile)
	}

	// Check data exists after compaction.
	for i := LSN(0); i < 10; i++ {
		curRec, err := log.getLogRec(log.s.id, GenID(1), i)
		if err != nil || curRec == nil {
			t.Fatalf("GetLogRec() can not find object %s:1:%d in log file %s, err %v",
				log.s.id, i, logfile, err)
		}
		if curRec.CurVers != storage.Version(i) {
			t.Errorf("Data mismatch for logrec %s:1:%d in log file %s: %v",
				log.s.id, i, logfile, curRec)
		}
	}

	for i := uint32(0); i < 10; i++ {
		curVal, err := log.getGenMetadata(log.s.id, GenID(i+10))
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%d in log file %s, err %v",
				log.s.id, i, logfile, err)
		}
		if curVal.Pos != i || curVal.Count != uint64(i+100) {
			t.Errorf("Data mismatch for object %s:%d in log file %s: %v",
				log.s.id, i, logfile, curVal)
		}
	}

	log.head.Curgen = 0
	log.head.Curlsn = 0
	log.head.Curorder = 0

	if err := log.getHead(); err != nil {
		t.Fatalf("getHead() can not find head in log file %s, err %v", logfile, err)
	}

	if log.head.Curgen != 1000 && log.head.Curlsn != 10 && log.head.Curorder != 100 {
		t.Errorf("Data mismatch for head in log file %s: %v", logfile, log.head)
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}
