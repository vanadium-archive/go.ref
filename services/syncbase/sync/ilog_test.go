// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for the Veyron Sync ILog component.
import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"testing"
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
		//t.Errorf("Log file %s not created", logfile)
	}

	if err := log.flush(); err != nil {
		t.Errorf("Cannot flush ILog file %s, err %v", logfile, err)
	}

	oldfsize := fsize
	fsize = getFileSize(logfile)
	if fsize <= oldfsize {
		//t.Errorf("Log file %s not flushed", logfile)
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close ILog file %s, err %v", logfile, err)
	}

	oldfsize = getFileSize(logfile)

	log, err = openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot re-open existing log file %s, err %v", logfile, err)
	}

	fsize = getFileSize(logfile)
	if fsize != oldfsize {
		t.Errorf("Log file %s size changed across re-open (%d %d)", logfile, fsize, oldfsize)
	}

	if err := log.flush(); err != nil {
		t.Errorf("Cannot flush ILog file %s, err %v", logfile, err)
	}

	if err := log.close(); err != nil {
		t.Errorf("Cannot close ILog file %s, err %v", logfile, err)
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

	validateError := func(err error, funcName string) {
		_, file, line, _ := runtime.Caller(1)
		if err == nil || err != errInvalidLog {
			t.Errorf("%s:%d %s() did not fail on a closed log: %v", file, line, funcName, err)
		}
	}

	err = log.close()
	validateError(err, "close")

	err = log.flush()
	validateError(err, "flush")

	srid := ObjId("haha")

	err = log.initSyncRoot(srid)
	validateError(err, "initSyncRoot")

	_, err = log.putLogRec(&LogRec{})
	validateError(err, "putLogRec")

	var devid DeviceId = "VeyronPhone"
	var gnum GenId = 1
	var lsn SeqNum

	_, err = log.getLogRec(devid, srid, gnum, lsn)
	validateError(err, "getLogRec")

	if log.hasLogRec(devid, srid, gnum, lsn) {
		t.Errorf("hasLogRec() did not fail on a closed log: %v", err)
	}

	err = log.delLogRec(devid, srid, gnum, lsn)
	validateError(err, "delLogRec")

	_, err = log.getGenMetadata(devid, srid, gnum)
	validateError(err, "getGenMetadata")

	err = log.delGenMetadata(devid, srid, gnum)
	validateError(err, "delGenMetadata")

	err = log.createRemoteGeneration(devid, srid, gnum, &genMetadata{})
	validateError(err, "createRemoteGeneration")

	_, err = log.createLocalGeneration(srid)
	validateError(err, "createLocalGeneration")

	err = log.processWatchRecord(ObjId("foobar"), 2, Version(999), &LogValue{}, srid)
	validateError(err, "processWatchRecord")

	// Harmless NOP.
	log.dump()
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
	if len(log.head.CurSRGens) != 0 || log.head.Curorder != 0 {
		t.Errorf("First time log create should reset header")
	}

	// No head should be there in db.
	if err = log.getHead(); err == nil {
		t.Errorf("getHead() found non-existent head in log file %s, err %v", logfile, err)
	}

	if log.hasHead() {
		t.Errorf("hasHead() found non-existent head in log file %s", logfile)
	}

	expVal := map[ObjId]*curLocalGen{ObjId("bla"): &curLocalGen{CurGenNum: 10, CurSeqNum: 100},
		ObjId("qwerty"): &curLocalGen{CurGenNum: 40, CurSeqNum: 80}}

	log.head = &iLogHeader{
		CurSRGens: expVal,
		Curorder:  1000,
	}

	if err := log.putHead(); err != nil {
		t.Errorf("Cannot put head %v in log file %s, err %v", log.head, logfile, err)
	}

	// Reset values.
	log.head.CurSRGens = nil
	log.head.Curorder = 0

	for i := 0; i < 2; i++ {
		if err := log.getHead(); err != nil {
			t.Fatalf("getHead() can not find head (i=%d) in log file %s, err %v", i, logfile, err)
		}

		if !log.hasHead() {
			t.Errorf("hasHead() can not find head (i=%d) in log file %s", i, logfile)
		}

		if !reflect.DeepEqual(log.head.CurSRGens, expVal) || log.head.Curorder != 1000 {
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
	if len(log.head.CurSRGens) != 0 || log.head.Curorder != 0 {
		t.Errorf("First time log create should reset header")
	}

	expVal := map[ObjId]*curLocalGen{ObjId("blabla"): &curLocalGen{CurGenNum: 10, CurSeqNum: 100},
		ObjId("xyz"): &curLocalGen{CurGenNum: 40, CurSeqNum: 80}}
	log.head = &iLogHeader{
		CurSRGens: expVal,
		Curorder:  1000,
	}

	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}

	log, err = openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	// In memory head should be initialized from db.
	if !reflect.DeepEqual(log.head.CurSRGens, expVal) || log.head.Curorder != 1000 {
		t.Errorf("Data mismatch for head in log file %s: %v", logfile, log.head)
	}

	for sr := range expVal {
		expVal[sr] = &curLocalGen{CurGenNum: 1000, CurSeqNum: 10}
		break
	}
	log.head = &iLogHeader{
		CurSRGens: expVal,
		Curorder:  100,
	}

	if err := log.flush(); err != nil {
		t.Errorf("Cannot flush ILog file %s, err %v", logfile, err)
	}

	// Reset values.
	log.head.CurSRGens = nil
	log.head.Curorder = 0

	if err := log.getHead(); err != nil {
		t.Fatalf("getHead() can not find head in log file %s, err %v", logfile, err)
	}

	// In memory head should be initialized from db.
	if !reflect.DeepEqual(log.head.CurSRGens, expVal) || log.head.Curorder != 100 {
		t.Errorf("Data mismatch for head in log file %s: %v", logfile, log.head)
	}

	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestLogInitDelSyncRoot tests initing and deleting a new SyncRoot.
func TestLogInitDelSyncRoot(t *testing.T) {
	logfile := getFileName()
	defer os.Remove(logfile)

	log, err := openILog(logfile, nil)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	srid1 := ObjId("haha")
	if err := log.initSyncRoot(srid1); err != nil {
		t.Fatalf("Cannot create new SyncRoot %s, err %v", srid1.String(), err)
	}
	srid2 := ObjId("asdf")
	if err := log.initSyncRoot(srid2); err != nil {
		t.Fatalf("Cannot create new SyncRoot %s, err %v", srid2.String(), err)
	}
	if err := log.initSyncRoot(srid2); err == nil {
		t.Fatalf("Creating existing SyncRoot didn't fail %s, err %v", srid2.String(), err)
	}
	expGen := &curLocalGen{CurGenNum: 1, CurSeqNum: 0}
	expSRGens := map[ObjId]*curLocalGen{
		srid1: expGen,
		srid2: expGen,
	}
	if !reflect.DeepEqual(log.head.CurSRGens, expSRGens) {
		t.Errorf("Data mismatch for head in log file %s: %v instead of %v",
			logfile, log.head.CurSRGens, expSRGens)
	}

	if err := log.delSyncRoot(srid1); err != nil {
		t.Fatalf("Cannot delete SyncRoot %s, err %v", srid1.String(), err)
	}
	if err := log.delSyncRoot(srid2); err != nil {
		t.Fatalf("Cannot delete SyncRoot %s, err %v", srid1.String(), err)
	}
	if err := log.delSyncRoot(srid1); err == nil {
		t.Fatalf("Deleting non-existent SyncRoot didn't fail %s", srid1.String())
	}
	expSRGens = map[ObjId]*curLocalGen{}
	if !reflect.DeepEqual(log.head.CurSRGens, expSRGens) {
		t.Errorf("Data mismatch for head in log file %s: %v instead of %v",
			logfile, log.head.CurSRGens, expSRGens)
	}

	if err := log.putHead(); err != nil {
		t.Errorf("Cannot put head %v in log file %s, err %v", log.head, logfile, err)
	}

	// Reset values.
	log.head.CurSRGens = nil
	log.head.Curorder = 0

	if err := log.getHead(); err != nil {
		t.Fatalf("getHead() can not find head in log file %s, err %v", logfile, err)
	}

	if !reflect.DeepEqual(log.head.CurSRGens, expSRGens) {
		t.Errorf("Data mismatch for head in log file %s: %v instead of %v",
			logfile, log.head.CurSRGens, expSRGens)
	}
}

// TestStringAndParseKeys tests conversion to/from string for log record and generation keys.
func TestStringAndParseKeys(t *testing.T) {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	randString := func(n int) string {
		b := make([]rune, n)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
		}
		return string(b)
	}

	for i := 0; i < 10; i++ {
		devid := DeviceId(randString(rand.Intn(20) + 1))
		srid := ObjId(fmt.Sprintf("haha-%d", i))
		gnum := GenId(rand.Int63())
		lsn := SeqNum(rand.Int63())

		devid1, srid1, gnum1, lsn1, err := splitLogRecKey(logRecKey(devid, srid, gnum, lsn))
		if err != nil || devid1 != devid || srid1 != srid || gnum1 != gnum || lsn1 != lsn {
			t.Fatalf("LogRec conversion failed in iter %d: got %s %v %v %v want %s %v %v %v, err %v",
				i, devid1, srid1, gnum1, lsn1, devid, srid, gnum, lsn, err)
		}

		devid2, srid2, gnum2, err := splitGenerationKey(generationKey(devid, srid, gnum))
		if err != nil || devid2 != devid || srid2 != srid || gnum2 != gnum {
			t.Fatalf("Gen conversion failed in iter %d: got %s %v %v want %s %v %v, err %v",
				i, devid2, srid2, gnum2, devid, srid, gnum, err)
		}
	}

	//badStrs := []string{"abc:3:4", "abc:123:4:5", "abc:1234:-1:10", "ijk:7890:1000:-1", "abc:3:4:5:6", "abc::3:4:5"}
	badStrs := []string{"abc:3:4", "abc:1234:-1:10", "ijk:7890:1000:-1", "abc:3:4:5:6", "abc::3:4:5"}
	for _, s := range badStrs {
		if _, _, _, _, err := splitLogRecKey(s); err == nil {
			t.Fatalf("LogRec conversion didn't fail for str %s", s)
		}
	}

	//badStrs = []string{"abc:3", "abc:123:4", "abc:1234:-1", "abc:3:4:5", "abc:4::5"}
	badStrs = []string{"abc:3", "abc:1234:-1", "abc:3:4:5", "abc:4::5"}
	for _, s := range badStrs {
		if _, _, _, err := splitGenerationKey(s); err == nil {
			t.Fatalf("Gen conversion didn't fail for str %s", s)
		}
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

	var devid DeviceId = "VeyronTab"
	srid := ObjId("haha")
	var gnum GenId = 100
	var lsn SeqNum = 1000

	rec, err := log.getLogRec(devid, srid, gnum, lsn)
	if err == nil || rec != nil {
		t.Errorf("GetLogRec() found non-existent object %s:%s:%d:%d in log file %s: %v, err %v",
			devid, srid.String(), gnum, lsn, logfile, rec, err)
	}

	if log.hasLogRec(devid, srid, gnum, lsn) {
		t.Errorf("HasLogRec() found non-existent object %s:%s:%d:%d in log file %s",
			devid, srid.String(), gnum, lsn, logfile)
	}

	rec = &LogRec{
		DevId:      devid,
		SyncRootId: srid,
		GenNum:     gnum,
		SeqNum:     lsn,
		ObjId:      ObjId("bla"),
		CurVers:    2,
		Parents:    []Version{0, 1},
		Value:      LogValue{},
	}

	if _, err := log.putLogRec(rec); err != nil {
		t.Errorf("Cannot put object %s:%s:%d:%d (%v) in log file %s, err %v", devid, srid.String(), gnum, lsn, rec, logfile, err)
	}

	for i := 0; i < 2; i++ {
		curRec, err := log.getLogRec(devid, srid, gnum, lsn)
		if err != nil || curRec == nil {
			t.Fatalf("GetLogRec() can not find object %s:%s:%d:%d (i=%d) in log file %s, err %v",
				devid, srid.String(), gnum, lsn, i, logfile, err)
		}

		if !log.hasLogRec(devid, srid, gnum, lsn) {
			t.Errorf("HasLogRec() can not find object %s:%s:%d:%d (i=%d) in log file %s",
				devid, srid.String(), gnum, lsn, i, logfile)
		}

		if !reflect.DeepEqual(curRec, rec) {
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

	log.dump()
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

	var devid DeviceId = "VeyronTab"
	srid := ObjId("haha")
	var gnum GenId = 100
	var lsn SeqNum = 1000

	rec := &LogRec{
		DevId:      devid,
		SyncRootId: srid,
		GenNum:     gnum,
		SeqNum:     lsn,
		ObjId:      ObjId("bla"),
		CurVers:    2,
		Parents:    []Version{0, 1},
		Value:      LogValue{},
	}

	if _, err := log.putLogRec(rec); err != nil {
		t.Errorf("Cannot put object %s:%s:%d:%d (%v) in log file %s, err %v", devid, srid.String(), gnum, lsn, rec, logfile, err)
	}

	curRec, err := log.getLogRec(devid, srid, gnum, lsn)
	if err != nil || curRec == nil {
		t.Fatalf("GetLogRec() can not find object %s:%s:%d:%d in log file %s, err %v",
			devid, srid.String(), gnum, lsn, logfile, err)
	}

	if err := log.delLogRec(devid, srid, gnum, lsn); err != nil {
		t.Fatalf("DelLogRec() can not delete object %s:%s:%d:%d in log file %s, err %v",
			devid, srid.String(), gnum, lsn, logfile, err)
	}

	for i := 0; i < 2; i++ {
		curRec, err = log.getLogRec(devid, srid, gnum, lsn)
		if err == nil || curRec != nil {
			t.Fatalf("GetLogRec() finds deleted object %s:%s:%d:%d (i=%d) in log file %s, err %v",
				devid, srid.String(), gnum, lsn, i, logfile, err)
		}

		if log.hasLogRec(devid, srid, gnum, lsn) {
			t.Errorf("HasLogRec() finds deleted object %s:%s:%d:%d (i=%d) in log file %s",
				devid, srid.String(), gnum, lsn, i, logfile)
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

	log.dump()
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

	var devid DeviceId = "VeyronTab"
	srid := ObjId("haha")
	var gnum GenId = 100

	val, err := log.getGenMetadata(devid, srid, gnum)
	if err == nil || val != nil {
		t.Errorf("GetGenMetadata() found non-existent object %s:%s:%d in log file %s: %v, err %v",
			devid, srid.String(), gnum, logfile, val, err)
	}

	if log.hasGenMetadata(devid, srid, gnum) {
		t.Errorf("hasGenMetadata() found non-existent object %s:%s:%d in log file %s",
			devid, srid.String(), gnum, logfile)
	}

	val = &genMetadata{Pos: 40, Count: 100, MaxSeqNum: 99}
	if err := log.putGenMetadata(devid, srid, gnum, val); err != nil {
		t.Errorf("Cannot put object %s:%s:%d in log file %s, err %v", devid, srid.String(), gnum, logfile, err)
	}

	for i := 0; i < 2; i++ {
		curVal, err := log.getGenMetadata(devid, srid, gnum)
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%s:%d (i=%d) in log file %s, err %v",
				devid, srid.String(), gnum, i, logfile, err)
		}

		if !log.hasGenMetadata(devid, srid, gnum) {
			t.Errorf("hasGenMetadata() can not find object %s:%s:%d (i=%d) in log file %s",
				devid, srid.String(), gnum, i, logfile)
		}

		if !reflect.DeepEqual(curVal, val) {
			t.Errorf("Data mismatch for object %s:%s:%d (i=%d) in log file %s: %v instead of %v",
				devid, srid.String(), gnum, i, logfile, curVal, val)
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

	log.dump()
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

	var devid DeviceId = "VeyronTab"
	srid := ObjId("haha")
	var gnum GenId = 100

	val := &genMetadata{Pos: 40, Count: 100, MaxSeqNum: 99}
	if err := log.putGenMetadata(devid, srid, gnum, val); err != nil {
		t.Errorf("Cannot put object %s:%s:%d in log file %s, err %v", devid, srid.String(), gnum, logfile, err)
	}

	curVal, err := log.getGenMetadata(devid, srid, gnum)
	if err != nil || curVal == nil {
		t.Fatalf("GetGenMetadata() can not find object %s:%s:%d in log file %s, err %v",
			devid, srid.String(), gnum, logfile, err)
	}

	if err := log.delGenMetadata(devid, srid, gnum); err != nil {
		t.Fatalf("DelGenMetadata() can not delete object %s:%s:%d in log file %s, err %v",
			devid, srid.String(), gnum, logfile, err)
	}

	for i := 0; i < 2; i++ {
		curVal, err := log.getGenMetadata(devid, srid, gnum)
		if err == nil || curVal != nil {
			t.Fatalf("GetGenMetadata() finds deleted object %s:%s:%d (i=%d) in log file %s, err %v",
				devid, srid.String(), gnum, i, logfile, err)
		}

		if log.hasGenMetadata(devid, srid, gnum) {
			t.Errorf("hasGenMetadata() finds deleted object %s:%s:%d (i=%d) in log file %s",
				devid, srid.String(), gnum, i, logfile)
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

	log.dump()
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

	var devid DeviceId = "VeyronTab"
	srid := ObjId("haha")

	// Add several generations.
	for i := uint32(0); i < 10; i++ {
		val := &genMetadata{Pos: i}
		if err := log.putGenMetadata(devid, srid, GenId(i+10), val); err != nil {
			t.Errorf("Cannot put object %s:%s:%d in log file %s, err %v", devid, srid.String(),
				i, logfile, err)
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
		curVal, err := log.getGenMetadata(devid, srid, GenId(i+10))
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%s:%d in log file %s, err %v",
				devid, srid.String(), i, logfile, err)
		}
		if curVal.Pos != i {
			t.Errorf("Data mismatch for object %s:%s:%d in log file %s: %v",
				devid, srid.String(), i, logfile, curVal)
		}
		// Should safely overwrite the same keys.
		curVal.Pos = i + 10
		if err := log.putGenMetadata(devid, srid, GenId(i+10), curVal); err != nil {
			t.Errorf("Cannot put object %s:%s:%d in log file %s, err %v", devid, srid.String(),
				i, logfile, err)
		}
	}
	for i := uint32(0); i < 10; i++ {
		curVal, err := log.getGenMetadata(devid, srid, GenId(i+10))
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%s:%d in log file %s, err %v",
				devid, srid.String(), i, logfile, err)
		}
		if curVal.Pos != (i + 10) {
			t.Errorf("Data mismatch for object %s:%s:%d in log file %s: %v, err %v",
				devid, srid.String(), i, logfile, curVal, err)
		}
	}

	log.dump()
	if err := log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// fillFakeLogRecords fills fake log records for testing purposes.
func (l *iLog) fillFakeLogRecords(srid ObjId) {
	const num = 10
	var parvers []Version
	id := ObjId("haha")
	for i := int(0); i < num; i++ {
		// Create a local log record.
		curvers := Version(i)
		rec, err := l.createLocalLogRec(id, curvers, parvers, &LogValue{}, srid)
		if err != nil {
			return
		}
		// Insert the new log record into the log.
		_, err = l.putLogRec(rec)
		if err != nil {
			return
		}
		parvers = []Version{curvers}
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

	// Try using a wrong SyncRoot ID.
	bad_srid := ObjId("xyz")
	if _, err := log.createLocalLogRec(ObjId("asdf"), NoVersion, nil, &LogValue{}, bad_srid); err == nil {
		t.Errorf("createLocalLogRec did not fail when using an invalid SyncRoot ID %v", bad_srid)
	}
	if _, err := log.createLocalLinkLogRec(ObjId("qwerty"), NoVersion, NoVersion, bad_srid); err == nil {
		t.Errorf("createLocalLinkLogRec did not fail when using an invalid SyncRoot ID %v", bad_srid)
	}
	if _, err := log.createLocalGeneration(bad_srid); err == nil {
		t.Errorf("createLocalGeneration did not fail when using an invalid SyncRoot ID %v", bad_srid)
	}

	srids := []ObjId{ObjId("foo"), ObjId("bar")}
	num := []uint64{10, 20}
	for pos, sr := range srids {
		if err := log.initSyncRoot(sr); err != nil {
			t.Fatalf("Cannot create new SyncRoot %s, err %v", sr.String(), err)
		}
		if g, err := log.createLocalGeneration(sr); err != errNoUpdates {
			t.Errorf("Should not find local updates gen %d with error %v", g, err)
		}

		var parvers []Version
		id := ObjId(fmt.Sprintf("haha-%d", pos))
		for i := uint64(0); i < num[pos]; i++ {
			// Create a local log record.
			curvers := Version(i)
			rec, err := log.createLocalLogRec(id, curvers, parvers, &LogValue{}, sr)
			if err != nil {
				t.Fatalf("Cannot create local log rec ObjId: %v Current: %v Parents: %v Error: %v",
					id, curvers, parvers, err)
			}

			temprec := &LogRec{
				DevId:      log.s.id,
				SyncRootId: sr,
				GenNum:     GenId(1),
				SeqNum:     SeqNum(i),
				ObjId:      id,
				CurVers:    curvers,
				Parents:    parvers,
				Value:      LogValue{},
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

			parvers = []Version{curvers}
		}
	}
	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}

	log, err = openILog(logfile, s)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}

	for pos, sr := range srids {
		if log.head.CurSRGens[sr].CurGenNum != 1 || log.head.CurSRGens[sr].CurSeqNum != SeqNum(num[pos]) {
			t.Errorf("Data mismatch in log header %v (pos=%d)", log.head, pos)
		}

		g, err := log.createLocalGeneration(sr)
		if g != 1 || err != nil {
			t.Errorf("Could not create local generation srid %s gen %d (pos=%d) with error %v",
				sr.String(), g, pos, err)
		}
		curVal, err := log.getGenMetadata(log.s.id, sr, g)
		if err != nil || curVal == nil {
			t.Fatalf("GetGenMetadata() can not find object %s:%s:%d (pos=%d) in log file %s, err %v",
				log.s.id, sr.String(), g, pos, logfile, err)
		}
		expVal := &genMetadata{Pos: uint32(pos), Count: num[pos], MaxSeqNum: SeqNum(num[pos] - 1)}
		if !reflect.DeepEqual(curVal, expVal) {
			t.Errorf("Data mismatch for object %s:%s:%d (pos=%d) in log file %s: %v instead of %v",
				log.s.id, sr.String(), g, pos, logfile, curVal, expVal)
		}
		if log.head.CurSRGens[sr].CurGenNum != 2 || log.head.CurSRGens[sr].CurSeqNum != 0 ||
			log.head.Curorder != uint32(pos+1) {
			t.Errorf("Data mismatch in log header (pos=%d) %v %v",
				pos, log.head.CurSRGens[sr], log.head.Curorder)
		}

		if g, err := log.createLocalGeneration(sr); err != errNoUpdates {
			t.Errorf("Should not find local updates sr %s gen %d (pos=%d) with error %v",
				sr.String(), g, pos, err)
		}
	}

	// Populate one more generation for only one syncroot.
	log.fillFakeLogRecords(srids[0])

	if g, err := log.createLocalGeneration(srids[0]); g != 2 || err != nil {
		t.Errorf("Could not create local generation sgid %s gen %d with error %v",
			srids[0].String(), g, err)
	}

	if g, err := log.createLocalGeneration(srids[1]); err != errNoUpdates {
		t.Errorf("Should not find local updates sr %s gen %d with error %v", srids[1].String(), g, err)
	}

	// Create a remote generation.
	expGen := &genMetadata{Count: 1, MaxSeqNum: 50}
	if err = log.createRemoteGeneration("VeyronPhone", srids[0], 1, expGen); err == nil {
		t.Errorf("Remote generation create should have failed")
	}
	expGen.MaxSeqNum = 0
	if err = log.createRemoteGeneration("VeyronPhone", srids[0], 1, expGen); err != nil {
		t.Errorf("createRemoteGeneration failed with err %v", err)
	}
	if expGen.Pos != 3 {
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

	gens := []GenId{2, 1}
	order := map[ObjId][]uint32{srids[0]: []uint32{0, 2},
		srids[1]: []uint32{1}}
	for pos, sr := range srids {
		for g := GenId(1); g <= gens[pos]; g++ {
			// Check all log records.
			for i := SeqNum(0); i < SeqNum(num[pos]); i++ {
				curRec, err := log.getLogRec(log.s.id, sr, g, i)
				if err != nil || curRec == nil {
					t.Fatalf("GetLogRec() can not find object %s:%s:%d:%d in log file %s, err %v",
						log.s.id, sr.String(), g, i, logfile, err)
				}
			}
			// Check generation metadata.
			curVal, err := log.getGenMetadata(log.s.id, sr, g)
			if err != nil || curVal == nil {
				t.Fatalf("GetGenMetadata() can not find object %s:%s:%d in log file %s, err %v",
					log.s.id, sr.String(), g, logfile, err)
			}
			expVal := &genMetadata{Count: num[pos], MaxSeqNum: SeqNum(num[pos] - 1), Pos: order[sr][g-1]}
			if !reflect.DeepEqual(curVal, expVal) {
				t.Errorf("Data mismatch for object %s:%s:%d in log file %s: %v instead of %v",
					log.s.id, sr.String(), g, logfile, curVal, expVal)
			}
		}
	}

	// Check remote generation metadata.
	curVal, err := log.getGenMetadata("VeyronPhone", srids[0], 1)
	if err != nil || curVal == nil {
		t.Fatalf("GetGenMetadata() can not find object in log file %s, err %v",
			logfile, err)
	}
	if !reflect.DeepEqual(curVal, expGen) {
		t.Errorf("Data mismatch for object in log file %s: %v instead of %v",
			logfile, curVal, expGen)
	}

	expSRGens := map[ObjId]*curLocalGen{srids[0]: &curLocalGen{3, 0},
		srids[1]: &curLocalGen{2, 0}}

	if !reflect.DeepEqual(log.head.CurSRGens, expSRGens) || log.head.Curorder != 4 {
		t.Errorf("Data mismatch in log header %v %v", log.head.CurSRGens, log.head.Curorder)
	}

	log.dump()
	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}

// TestProcessWatchRecord tests that local updates are correctly handled.
// Commands are in file testdata/<local-init-00.log.sync, local-init-02.log.sync>.
/*
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

	srids := []ObjId{"foo", "bar"}
	fnames := []string{"local-init-00.log.sync", "local-init-02.log.sync"}
	num := []uint32{3, 5}

	// Try using an invalid SyncRoot ID.
	if err = log.processWatchRecord(ObjId("haha"), 2, Version(999), &LogValue{}, NoObjId); err == nil {
		t.Errorf("processWatchRecord did not fail when using an invalid SyncRoot ID")
	}

	// Test echo suppression on JoinSyncGroup.
	if err := log.processWatchRecord(srids[0], 5, NoVersion, &LogValue{}, srids[0]); err != nil {
		t.Fatalf("Echo suppression on JoinSyncGroup failed with err %v", err)
	}

	for pos, sr := range srids {
		if err := log.initSyncRoot(sr); err != nil {
			t.Fatalf("Cannot create new SyncRoot %s, err %v", sr.String(), err)
		}

		if err := logReplayCommands(log, fnames[pos], sr); err != nil {
			t.Error(err)
		}
	}

	checkObject := func(obj string, expHead Version) {
		_, file, line, _ := runtime.Caller(1)
		objid, err := strToObjId(obj)
		if err != nil {
			t.Errorf("%s:%d Could not create objid %v", file, line, err)
		}

		// Verify DAG state.
		if head, err := log.s.dag.getHead(objid); err != nil || head != expHead {
			t.Errorf("%s:%d Invalid object %d head in DAG %v want %v, err %v", file, line,
				objid, head, expHead, err)
		}
	}
	for pos, sr := range srids {
		if log.head.CurSRGens[sr].CurGenNum != 1 || log.head.CurSRGens[sr].CurSeqNum != SeqNum(num[pos]) ||
			log.head.Curorder != 0 {
			t.Errorf("Data mismatch in log header %v", log.head)
		}

		// Check all log records.
		for i := SeqNum(0); i < SeqNum(num[pos]); i++ {
			curRec, err := log.getLogRec(log.s.id, sr, GenId(1), i)
			if err != nil || curRec == nil {
				t.Fatalf("GetLogRec() can not find object %s:%s:1:%d in log file %s, err %v",
					log.s.id, sr.String(), i, logfile, err)
			}
		}

		if pos == 0 {
			checkObject("1234", 3)
		} else if pos == 1 {
			checkObject("12", 3)
			checkObject("45", 20)
		}
	}

	s.dag.flush()
	s.dag.close()

	log.dump()
	if err = log.close(); err != nil {
		t.Errorf("Cannot close log file %s, err %v", logfile, err)
	}
}
*/
