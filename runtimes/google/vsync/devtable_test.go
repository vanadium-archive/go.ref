package vsync

// Tests for the Veyron Sync devTable component.
import (
	"os"
	"reflect"
	"testing"
	"time"
)

// TestDevTabStore tests creating a backing file for devTable.
func TestDevTabStore(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	fsize := getFileSize(devfile)
	if fsize < 0 {
		t.Errorf("DevTable file %s not created", devfile)
	}

	if err := dtab.flush(); err != nil {
		t.Errorf("Cannot flush devTable file %s, err %v", devfile, err)
	}

	oldfsize := fsize
	fsize = getFileSize(devfile)
	if fsize <= oldfsize {
		t.Errorf("DevTable file %s not flushed", devfile)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}

	oldfsize = getFileSize(devfile)

	dtab, err = openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot re-open existing devTable file %s, err %v", devfile, err)
	}

	fsize = getFileSize(devfile)
	if fsize != oldfsize {
		t.Errorf("DevTable file %s size changed across re-open (%d %d)", devfile, fsize, oldfsize)
	}

	if err := dtab.flush(); err != nil {
		t.Errorf("Cannot flush devTable file %s, err %v", devfile, err)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestInvalidDTab tests devTable methods on an invalid (closed) devTable ptr.
func TestInvalidDTab(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}

	err = dtab.close()
	if err == nil || err != errInvalidDTab {
		t.Errorf("Close did not fail on a closed : %v", err)
	}

	err = dtab.flush()
	if err == nil || err != errInvalidDTab {
		t.Errorf("Flush did not fail on a closed devTable: %v", err)
	}

	err = dtab.compact()
	if err == nil || err != errInvalidDTab {
		t.Errorf("Compact did not fail on a closed devTable: %v", err)
	}

	var devid DeviceID = "VeyronPhone"

	err = dtab.putDevInfo(devid, &devInfo{})
	if err == nil || err != errInvalidDTab {
		t.Errorf("PutDevInfo did not fail on a closed devTable: %v", err)
	}

	_, err = dtab.getDevInfo(devid)
	if err == nil || err != errInvalidDTab {
		t.Errorf("GetDevInfo did not fail on a closed devTable: %v", err)
	}

	if dtab.hasDevInfo(devid) {
		if err == nil || err != errInvalidDTab {
			t.Errorf("HasDevInfo did not fail on a closed devTable: %v", err)
		}
	}

	err = dtab.putGenVec(devid, GenVector{})
	if err == nil || err != errInvalidDTab {
		t.Errorf("PutGenVec did not fail on a closed devTable: %v", err)
	}

	_, err = dtab.getGenVec(devid)
	if err == nil || err != errInvalidDTab {
		t.Errorf("GetGenVec did not fail on a closed devTable: %v", err)
	}

	err = dtab.updateGeneration(devid, devid, 0)
	if err == nil || err != errInvalidDTab {
		t.Errorf("UpdateGeneration did not fail on a closed devTable: %v", err)
	}

	err = dtab.updateLocalGenVector(GenVector{}, GenVector{})
	if err == nil || err != errInvalidDTab {
		t.Errorf("UpdateLocalGenVector did not fail on a closed devTable: %v", err)
	}

	_, err = dtab.diffGenVectors(GenVector{}, GenVector{})
	if err == nil || err != errInvalidDTab {
		t.Errorf("DiffGenVectors did not fail on a closed devTable: %v", err)
	}
}

// TestPutGetDevTableHeader tests setting and getting devTable header across devTable open/close/reopen.
func TestPutGetDevTableHeader(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	// In memory head should be initialized.
	if dtab.head.Resmark != nil {
		t.Errorf("First time log create should reset header: %v", dtab.head.Resmark)
	}
	expVec := GenVector{dtab.s.id: 0}
	if !reflect.DeepEqual(dtab.head.ReclaimVec, expVec) {
		t.Errorf("Data mismatch for reclaimVec in devTable file %s: %v instead of %v",
			devfile, dtab.head.ReclaimVec, expVec)
	}

	// No head should be there in db.
	if err = dtab.getHead(); err == nil {
		t.Errorf("getHead() found non-existent head in devTable file %s, err %v", devfile, err)
	}

	if dtab.hasHead() {
		t.Errorf("hasHead() found non-existent head in devTable file %s", devfile)
	}

	expMark := []byte{1, 2, 3}
	expVec = GenVector{
		"VeyronTab":   30,
		"VeyronPhone": 10,
	}
	dtab.head = &devTableHeader{
		Resmark:    expMark,
		ReclaimVec: expVec,
	}

	if err := dtab.putHead(); err != nil {
		t.Errorf("Cannot put head %v in devTable file %s, err %v", dtab.head, devfile, err)
	}

	// Reset values.
	dtab.head.Resmark = nil
	dtab.head.ReclaimVec = GenVector{}

	for i := 0; i < 2; i++ {
		if err := dtab.getHead(); err != nil {
			t.Fatalf("getHead() can not find head (i=%d) in devTable file %s, err %v", i, devfile, err)
		}

		if !dtab.hasHead() {
			t.Errorf("hasHead() can not find head (i=%d) in devTable file %s", i, devfile)
		}

		if !reflect.DeepEqual(dtab.head.Resmark, expMark) {
			t.Errorf("Data mismatch for resmark (i=%d) in devTable file %s: %v instead of %v",
				i, devfile, dtab.head.Resmark, expMark)
		}
		if !reflect.DeepEqual(dtab.head.ReclaimVec, expVec) {
			t.Errorf("Data mismatch for reclaimVec (i=%d) in devTable file %s: %v instead of %v",
				i, devfile, dtab.head.ReclaimVec, expVec)
		}

		if i == 0 {
			if err := dtab.close(); err != nil {
				t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
			}
			dtab, err = openDevTable(devfile, s)
			if err != nil {
				t.Fatalf("Cannot re-open devTable file %s, err %v", devfile, err)
			}
		}
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestPersistDevTableHeader tests that devTable header is
// automatically persisted across devTable open/close/reopen.
func TestPersistDevTableHeader(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	// In memory head should be initialized.
	if dtab.head.Resmark != nil {
		t.Errorf("First time log create should reset header: %v", dtab.head.Resmark)
	}
	expVec := GenVector{dtab.s.id: 0}
	if !reflect.DeepEqual(dtab.head.ReclaimVec, expVec) {
		t.Errorf("Data mismatch for reclaimVec in devTable file %s: %v instead of %v",
			devfile, dtab.head.ReclaimVec, expVec)
	}

	expMark := []byte{0, 2, 255}
	expVec = GenVector{
		"VeyronTab":   100,
		"VeyronPhone": 10000,
	}
	dtab.head = &devTableHeader{
		Resmark:    expMark,
		ReclaimVec: expVec,
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}

	dtab, err = openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	// In memory head should be initialized from db.
	if !reflect.DeepEqual(dtab.head.Resmark, expMark) {
		t.Errorf("Data mismatch for resmark in devTable file %s: %v instead of %v",
			devfile, dtab.head.Resmark, expMark)
	}
	if !reflect.DeepEqual(dtab.head.ReclaimVec, expVec) {
		t.Errorf("Data mismatch for reclaimVec in devTable file %s: %v instead of %v",
			devfile, dtab.head.ReclaimVec, expVec)
	}

	expMark = []byte{60, 180, 7}
	expVec = GenVector{
		"VeyronTab":   1,
		"VeyronPhone": 1987,
	}
	dtab.head = &devTableHeader{
		Resmark:    expMark,
		ReclaimVec: expVec,
	}

	if err := dtab.flush(); err != nil {
		t.Errorf("Cannot flush devTable file %s, err %v", devfile, err)
	}

	// Reset values.
	dtab.head.Resmark = nil
	dtab.head.ReclaimVec = GenVector{}

	if err := dtab.getHead(); err != nil {
		t.Fatalf("getHead() can not find head in devTable file %s, err %v", devfile, err)
	}

	// In memory head should be initialized from db.
	if !reflect.DeepEqual(dtab.head.Resmark, expMark) {
		t.Errorf("Data mismatch for resmark in devTable file %s: %v instead of %v",
			devfile, dtab.head.Resmark, expMark)
	}
	if !reflect.DeepEqual(dtab.head.ReclaimVec, expVec) {
		t.Errorf("Data mismatch for reclaimVec in devTable file %s: %v instead of %v",
			devfile, dtab.head.ReclaimVec, expVec)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestPutGetDevInfo tests setting and getting devInfo across devTable open/close/reopen.
func TestPutGetDevInfo(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	var devid DeviceID = "VeyronTab"

	info, err := dtab.getDevInfo(devid)
	if err == nil || info != nil {
		t.Errorf("GetDevInfo() found non-existent object %s in devTable file %s: %v, err %v",
			devid, devfile, info, err)
	}

	if dtab.hasDevInfo(devid) {
		t.Errorf("HasDevInfo() found non-existent object %s in devTable file %s",
			devid, devfile)
	}

	v := GenVector{
		"VeyronTab":   0,
		"VeyronPhone": 10,
	}

	info = &devInfo{
		Vector: v,
		Ts:     time.Now().UTC(),
	}

	if err := dtab.putDevInfo(devid, info); err != nil {
		t.Errorf("Cannot put object %s (%v) in devTable file %s, err %v", devid, info, devfile, err)
	}

	for i := 0; i < 2; i++ {
		curInfo, err := dtab.getDevInfo(devid)
		if err != nil || curInfo == nil {
			t.Fatalf("GetDevInfo() can not find object %s (i=%d) in devTable file %s: %v, err: %v",
				devid, i, devfile, curInfo, err)
		}

		if !dtab.hasDevInfo(devid) {
			t.Errorf("HasDevInfo() can not find object %s (i=%d) in devTable file %s",
				devid, i, devfile)
		}

		if !reflect.DeepEqual(info, curInfo) {
			t.Errorf("Data mismatch for object %s (i=%d) in devTable file %s: %v instead of %v",
				devid, i, devfile, curInfo, info)
		}

		if i == 0 {
			if err := dtab.close(); err != nil {
				t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
			}
			dtab, err = openDevTable(devfile, s)
			if err != nil {
				t.Fatalf("Cannot re-open devTable file %s, err %v", devfile, err)
			}
		}
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestPutGetGenVec tests setting and getting generation vector across dtab open/close/reopen.
func TestPutGetGenVec(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	local := GenVector{
		"VeyronPhone": 0,
	}

	var devid DeviceID = "VeyronTab"
	vec, err := dtab.getGenVec(devid)
	if err == nil || vec != nil {
		t.Errorf("GetGenVec() found non-existent object %s in devTable file %s: %v, err %v",
			devid, devfile, vec, err)
	}

	v := GenVector{
		"VeyronTab":     0,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  2,
	}

	if err := dtab.putGenVec(devid, v); err != nil {
		t.Errorf("Cannot put object %s (%v) in devTable file %s, err %v", devid, v, devfile, err)
	}

	for i := 0; i < 2; i++ {
		// Check for devid.
		curVec, err := dtab.getGenVec(devid)
		if err != nil || curVec == nil {
			t.Fatalf("GetGenVec() can not find object %s (i=%d) in devTable file %s, err %v",
				devid, i, devfile, err)
		}

		if !reflect.DeepEqual(v, curVec) {
			t.Errorf("Data mismatch for object %s (i=%d) in devTable file %s: %v instead of %v",
				devid, i, devfile, curVec, v)
		}

		// Check for s.id.
		curLocal, err := dtab.getGenVec(s.id)
		if err != nil || curLocal == nil {
			t.Fatalf("GetGenVec() can not find object %s (i=%d) in devTable file %s: %v, err: %v",
				s.id, i, devfile, curLocal, err)
		}

		if !reflect.DeepEqual(local, curLocal) {
			t.Errorf("Data mismatch for object %s (i=%d) in devTable file %s: %v instead of %v",
				s.id, i, devfile, curLocal, local)
		}

		if i == 0 {
			if err := dtab.close(); err != nil {
				t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
			}
			dtab, err = openDevTable(devfile, s)
			if err != nil {
				t.Fatalf("Cannot re-open devTable file %s, err %v", devfile, err)
			}
		}
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestUpdateGeneration tests updating a generation.
func TestUpdateGeneration(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	var devid DeviceID = "VeyronTab"
	err = dtab.updateGeneration(devid, devid, 10)
	if err == nil {
		t.Errorf("UpdateGeneration() found non-existent object %s in devTable file %s, err %v",
			devid, devfile, err)
	}
	v := GenVector{
		"VeyronTab":     0,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  2,
	}

	if err := dtab.putGenVec(devid, v); err != nil {
		t.Errorf("Cannot put object %s (%v) in devTable file %s, err %v", devid, v, devfile, err)
	}
	err = dtab.updateGeneration(devid, devid, 10)
	if err != nil {
		t.Errorf("UpdateGeneration() failed for %s in devTable file %s with error %v",
			devid, devfile, err)
	}
	err = dtab.updateGeneration(devid, "VeyronLaptop", 18)
	if err != nil {
		t.Errorf("UpdateGeneration() failed for %s in devTable file %s with error %v",
			devid, devfile, err)
	}
	curVec, err := dtab.getGenVec(devid)
	if err != nil || curVec == nil {
		t.Fatalf("GetGenVec() can not find object %s in devTable file %s, err %v",
			devid, devfile, err)
	}
	vExp := GenVector{
		"VeyronTab":     10,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  18,
	}

	if !reflect.DeepEqual(curVec, vExp) {
		t.Errorf("Data mismatch for object %s in devTable file %s: %v instead of %v",
			devid, devfile, v, vExp)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestUpdateLocalGenVector tests updating a gen vector.
func TestUpdateLocalGenVector(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	// Test nil args.
	if err := dtab.updateLocalGenVector(nil, nil); err == nil {
		t.Errorf("UpdateLocalGenVector() failed in devTable file %s with error %v",
			devfile, err)
	}

	// Nothing to update.
	local := GenVector{
		"VeyronTab":   0,
		"VeyronPhone": 1,
	}
	remote := GenVector{
		"VeyronTab":   0,
		"VeyronPhone": 1,
	}
	if err := dtab.updateLocalGenVector(local, remote); err != nil {
		t.Errorf("UpdateLocalGenVector() failed in devTable file %s with error %v",
			devfile, err)
	}

	if !reflect.DeepEqual(local, remote) {
		t.Errorf("Data mismatch for object %v instead of %v",
			local, remote)
	}

	// local is missing a generation.
	local = GenVector{
		"VeyronPhone": 1,
	}
	if err := dtab.updateLocalGenVector(local, remote); err != nil {
		t.Errorf("UpdateLocalGenVector() failed in devTable file %s with error %v",
			devfile, err)
	}
	if !reflect.DeepEqual(local, remote) {
		t.Errorf("Data mismatch for object %v instead of %v",
			local, remote)
	}

	// local is stale compared to remote.
	local = GenVector{
		"VeyronTab":   0,
		"VeyronPhone": 0,
	}
	remote = GenVector{
		"VeyronTab":    1,
		"VeyronPhone":  0,
		"VeyronLaptop": 2,
	}
	if err := dtab.updateLocalGenVector(local, remote); err != nil {
		t.Errorf("UpdateLocalGenVector() failed in devTable file %s with error %v",
			devfile, err)
	}
	if !reflect.DeepEqual(local, remote) {
		t.Errorf("Data mismatch for object %v instead of %v",
			local, remote)
	}

	// local is partially stale.
	local = GenVector{
		"VeyronTab":     0,
		"VeyronPhone":   0,
		"VeyronDesktop": 20,
	}
	remote = GenVector{
		"VeyronTab":    1,
		"VeyronPhone":  10,
		"VeyronLaptop": 2,
	}
	localExp := GenVector{
		"VeyronTab":     1,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  2,
	}
	if err := dtab.updateLocalGenVector(local, remote); err != nil {
		t.Errorf("UpdateLocalGenVector() failed in devTable file %s with error %v",
			devfile, err)
	}
	if !reflect.DeepEqual(local, localExp) {
		t.Errorf("Data mismatch for object %v instead of %v",
			local, localExp)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestDiffGenVectors tests diffing gen vectors.
func TestDiffGenVectors(t *testing.T) {
	logOrder := []DeviceID{"VeyronTab", "VeyronPhone", "VeyronDesktop", "VeyronLaptop"}
	var expGens []*genOrder

	// set reclaimVec such that it doesn't affect diffs.
	reclaimVec := GenVector{
		"VeyronTab":     0,
		"VeyronPhone":   0,
		"VeyronDesktop": 0,
		"VeyronLaptop":  0,
	}

	// src and dest are identical vectors.
	vec := GenVector{
		"VeyronTab":     1,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  2,
	}
	setupAndTestDiff(t, vec, vec, reclaimVec, logOrder, expGens)

	// src has no updates.
	srcVec := GenVector{
		"VeyronTab": 0,
	}
	remoteVec := GenVector{
		"VeyronTab":     5,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  8,
	}
	setupAndTestDiff(t, srcVec, remoteVec, reclaimVec, []DeviceID{}, expGens)

	// src and remote have no updates.
	srcVec = GenVector{
		"VeyronTab": 0,
	}
	remoteVec = GenVector{
		"VeyronTab": 0,
	}
	setupAndTestDiff(t, srcVec, remoteVec, reclaimVec, []DeviceID{}, expGens)

	// set reclaimVec such that it doesn't affect diffs.
	reclaimVec = GenVector{
		"VeyronTab": 0,
	}

	// src is staler than remote.
	srcVec = GenVector{
		"VeyronTab":     1,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  2,
	}
	remoteVec = GenVector{
		"VeyronTab":     5,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  8,
	}
	setupAndTestDiff(t, srcVec, remoteVec, reclaimVec, logOrder, expGens)

	// src is fresher than remote.
	srcVec = GenVector{
		"VeyronTab":     5,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  2,
	}
	remoteVec = GenVector{
		"VeyronTab":     1,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  2,
	}
	expGens = make([]*genOrder, 4)
	for i := 0; i < 4; i++ {
		expGens[i] = &genOrder{
			devID: "VeyronTab",
			genID: GenID(i + 2),
			order: uint32(i + 1),
		}
	}
	setupAndTestDiff(t, srcVec, remoteVec, reclaimVec, logOrder, expGens)

	// src is fresher than remote in all but one device.
	srcVec = GenVector{
		"VeyronTab":     5,
		"VeyronPhone":   10,
		"VeyronDesktop": 22,
		"VeyronLaptop":  2,
	}
	remoteVec = GenVector{
		"VeyronTab":     1,
		"VeyronPhone":   10,
		"VeyronDesktop": 20,
		"VeyronLaptop":  2,
		"VeyronCloud":   40,
	}
	expGens = make([]*genOrder, 6)
	for i := 0; i < 6; i++ {
		switch {
		case i < 4:
			expGens[i] = &genOrder{
				devID: "VeyronTab",
				genID: GenID(i + 2),
				order: uint32(i + 1),
			}
		default:
			expGens[i] = &genOrder{
				devID: "VeyronDesktop",
				genID: GenID(i - 4 + 21),
				order: uint32(i - 4 + 35),
			}
		}
	}
	setupAndTestDiff(t, srcVec, remoteVec, reclaimVec, logOrder, expGens)

	// src is fresher than dest, scramble log order.
	o := []DeviceID{"VeyronTab", "VeyronLaptop", "VeyronPhone", "VeyronDesktop"}
	srcVec = GenVector{
		"VeyronTab":     1,
		"VeyronPhone":   2,
		"VeyronDesktop": 3,
		"VeyronLaptop":  4,
	}
	remoteVec = GenVector{
		"VeyronTab":     0,
		"VeyronPhone":   2,
		"VeyronDesktop": 0,
	}
	expGens = make([]*genOrder, 8)
	for i := 0; i < 8; i++ {
		switch {
		case i < 1:
			expGens[i] = &genOrder{
				devID: "VeyronTab",
				genID: GenID(i + 1),
				order: uint32(i),
			}
		case i >= 1 && i < 5:
			expGens[i] = &genOrder{
				devID: "VeyronLaptop",
				genID: GenID(i),
				order: uint32(i),
			}
		default:
			expGens[i] = &genOrder{
				devID: "VeyronDesktop",
				genID: GenID(i - 4),
				order: uint32(i - 5 + 7),
			}
		}
	}
	setupAndTestDiff(t, srcVec, remoteVec, reclaimVec, o, expGens)

	// remote has no updates.
	srcVec = GenVector{
		"VeyronTab":     1,
		"VeyronPhone":   2,
		"VeyronDesktop": 3,
		"VeyronLaptop":  4,
	}
	remoteVec = GenVector{
		"VeyronPhone": 0,
	}
	expGens = make([]*genOrder, 10)
	for i := 0; i < 10; i++ {
		switch {
		case i < 1:
			expGens[i] = &genOrder{
				devID: "VeyronTab",
				genID: GenID(i + 1),
				order: uint32(i),
			}
		case i >= 1 && i < 3:
			expGens[i] = &genOrder{
				devID: "VeyronPhone",
				genID: GenID(i),
				order: uint32(i),
			}
		case i >= 3 && i < 6:
			expGens[i] = &genOrder{
				devID: "VeyronDesktop",
				genID: GenID(i - 2),
				order: uint32(i),
			}
		default:
			expGens[i] = &genOrder{
				devID: "VeyronLaptop",
				genID: GenID(i - 5),
				order: uint32(i),
			}
		}
	}
	setupAndTestDiff(t, srcVec, remoteVec, reclaimVec, logOrder, expGens)

	// Test with reclaimVec fast-fwded.
	reclaimVec = GenVector{
		"VeyronPhone":  1,
		"VeyronLaptop": 2,
	}
	srcVec = GenVector{
		"VeyronTab":     1,
		"VeyronPhone":   2,
		"VeyronDesktop": 3,
		"VeyronLaptop":  4,
	}
	remoteVec = GenVector{
		"VeyronPhone": 0,
	}
	expGens = make([]*genOrder, 7)
	for i := 0; i < 7; i++ {
		switch {
		case i < 1:
			expGens[i] = &genOrder{
				devID: "VeyronTab",
				genID: GenID(i + 1),
				order: uint32(i),
			}
		case i == 1:
			expGens[i] = &genOrder{
				devID: "VeyronPhone",
				genID: GenID(i + 1),
				order: uint32(i + 1),
			}
		case i >= 2 && i < 5:
			expGens[i] = &genOrder{
				devID: "VeyronDesktop",
				genID: GenID(i - 1),
				order: uint32(i + 1),
			}
		default:
			expGens[i] = &genOrder{
				devID: "VeyronLaptop",
				genID: GenID(i - 2),
				order: uint32(i + 3),
			}
		}
	}
	setupAndTestDiff(t, srcVec, remoteVec, reclaimVec, logOrder, expGens)
}

// setupAndTestDiff is an utility function to test diffing generation vectors.
func setupAndTestDiff(t *testing.T, srcVec, remoteVec, reclaimVec GenVector, logOrder []DeviceID, expGens []*genOrder) {
	devfile := getFileName()
	defer os.Remove(devfile)

	logfile := getFileName()
	defer os.Remove(logfile)

	var srcid DeviceID = "VeyronTab"
	var destid DeviceID = "VeyronPhone"

	var err error
	s := &syncd{id: srcid}
	s.log, err = openILog(logfile, s)
	if err != nil {
		t.Fatalf("Cannot open new log file %s, err %v", logfile, err)
	}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}
	dtab.head.ReclaimVec = reclaimVec

	// Populate generations in log order.
	var order uint32
	for _, k := range logOrder {
		v, ok := (srcVec)[k]
		if !ok {
			t.Errorf("Cannot find key %s in srcVec %v", k, srcVec)
		}
		for i := GenID(1); i <= v; i++ {
			val := &genMetadata{Pos: order}
			if err := dtab.s.log.putGenMetadata(k, i, val); err != nil {
				t.Errorf("Cannot put object %s:%d in log file %s, err %v", k, v, logfile, err)
			}
			order++
		}
	}
	gens, err := dtab.diffGenVectors(srcVec, remoteVec)
	if err != nil {
		t.Fatalf("DiffGenVectors() failed src: %s %v dest: %s %v in devTable file %s, err %v",
			srcid, srcVec, destid, remoteVec, devfile, err)
	}

	if !reflect.DeepEqual(gens, expGens) {
		t.Fatalf("Data mismatch for genorder %v instead of %v, src %v dest %v reclaim %v",
			gens, expGens, srcVec, remoteVec, reclaimVec)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestGetOldestGen tests obtaining generations from reclaimVec.
func TestGetOldestGen(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	var srcid DeviceID = "VeyronTab"
	s := &syncd{id: srcid}
	var err error
	s.devtab, err = openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	if s.devtab.getOldestGen(srcid) != 0 {
		t.Errorf("Cannot get generation for device %s in devTable file %s",
			srcid, devfile)
	}

	var destid DeviceID = "VeyronPhone"
	if s.devtab.getOldestGen(destid) != 0 {
		t.Errorf("Cannot get generation for device %s in devTable file %s",
			destid, devfile)
	}

	s.devtab.head.ReclaimVec[srcid] = 10
	if s.devtab.getOldestGen(srcid) != 10 {
		t.Errorf("Cannot get generation for device %s in devTable file %s",
			srcid, devfile)
	}
	if s.devtab.getOldestGen(destid) != 0 {
		t.Errorf("Cannot get generation for device %s in devTable file %s",
			destid, devfile)
	}

	if err := s.devtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestComputeReclaimVector tests reclaim vector computation.
func TestComputeReclaimVector(t *testing.T) {
	devArr := []DeviceID{"VeyronTab", "VeyronPhone", "VeyronDesktop", "VeyronLaptop"}
	genVecArr := make([]GenVector, 4)

	// All devices are up-to-date.
	genVecArr[0] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[1] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[2] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[3] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	setupAndTestReclaimVector(t, devArr, genVecArr, nil, genVecArr[0])

	// Every device is missing at least one other device. Not possible to gc.
	genVecArr[0] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[1] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronLaptop": 4}
	genVecArr[2] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3}
	genVecArr[3] = GenVector{"VeyronDesktop": 3, "VeyronLaptop": 4}
	expReclaimVec := GenVector{"VeyronTab": 0, "VeyronPhone": 0, "VeyronDesktop": 0, "VeyronLaptop": 0}
	setupAndTestReclaimVector(t, devArr, genVecArr, nil, expReclaimVec)

	// All devices know at least one generation from other devices.
	genVecArr[0] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[1] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 2, "VeyronLaptop": 2}
	genVecArr[2] = GenVector{"VeyronTab": 1, "VeyronPhone": 1, "VeyronDesktop": 3, "VeyronLaptop": 1}
	genVecArr[3] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 1, "VeyronLaptop": 4}
	expReclaimVec = GenVector{"VeyronTab": 1, "VeyronPhone": 1, "VeyronDesktop": 1, "VeyronLaptop": 1}
	setupAndTestReclaimVector(t, devArr, genVecArr, nil, expReclaimVec)

	// One device is missing from one other device.
	genVecArr[0] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[1] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 2}
	genVecArr[2] = GenVector{"VeyronTab": 1, "VeyronPhone": 1, "VeyronDesktop": 3, "VeyronLaptop": 1}
	genVecArr[3] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 1, "VeyronLaptop": 4}
	expReclaimVec = GenVector{"VeyronTab": 1, "VeyronPhone": 1, "VeyronDesktop": 1, "VeyronLaptop": 0}
	setupAndTestReclaimVector(t, devArr, genVecArr, nil, expReclaimVec)

	// All devices know at least "n" generations from other devices.
	var n GenID = 10
	genVecArr[0] = GenVector{"VeyronTab": n + 10, "VeyronPhone": n,
		"VeyronDesktop": n + 8, "VeyronLaptop": n + 4}
	genVecArr[1] = GenVector{"VeyronTab": n + 6, "VeyronPhone": n + 10,
		"VeyronDesktop": n, "VeyronLaptop": n + 3}
	genVecArr[2] = GenVector{"VeyronTab": n, "VeyronPhone": n + 2,
		"VeyronDesktop": n + 10, "VeyronLaptop": n}
	genVecArr[3] = GenVector{"VeyronTab": n + 7, "VeyronPhone": n + 1,
		"VeyronDesktop": n + 5, "VeyronLaptop": n + 10}
	expReclaimVec = GenVector{"VeyronTab": n, "VeyronPhone": n, "VeyronDesktop": n, "VeyronLaptop": n}
	setupAndTestReclaimVector(t, devArr, genVecArr, nil, expReclaimVec)

	// Never contacted a device.
	devArr = []DeviceID{"VeyronTab", "VeyronDesktop", "VeyronLaptop"}
	genVecArr[0] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[1] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[2] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	expReclaimVec = GenVector{"VeyronTab": 0, "VeyronPhone": 0, "VeyronDesktop": 0, "VeyronLaptop": 0}
	setupAndTestReclaimVector(t, devArr, genVecArr, nil, expReclaimVec)

	// Start from existing reclaim vector.
	devArr = []DeviceID{"VeyronTab", "VeyronPhone", "VeyronDesktop", "VeyronLaptop"}
	reclaimVec := GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 3, "VeyronLaptop": 4}
	genVecArr[0] = GenVector{"VeyronTab": 6, "VeyronPhone": 6, "VeyronDesktop": 6, "VeyronLaptop": 6}
	genVecArr[1] = GenVector{"VeyronTab": 6, "VeyronPhone": 6, "VeyronDesktop": 3, "VeyronLaptop": 6}
	genVecArr[2] = GenVector{"VeyronTab": 6, "VeyronPhone": 6, "VeyronDesktop": 6, "VeyronLaptop": 4}
	genVecArr[3] = GenVector{"VeyronTab": 1, "VeyronPhone": 2, "VeyronDesktop": 6, "VeyronLaptop": 6}

	setupAndTestReclaimVector(t, devArr, genVecArr, reclaimVec, reclaimVec)
}

// setupAndTestReclaimVector is an utility function to test reclaim vector computation.
func setupAndTestReclaimVector(t *testing.T, devArr []DeviceID, genVecArr []GenVector, reclaimStart, expReclaimVec GenVector) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronTab"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}
	if reclaimStart != nil {
		dtab.head.ReclaimVec = reclaimStart
	}

	for i := range devArr {
		if err := dtab.putGenVec(devArr[i], genVecArr[i]); err != nil {
			t.Errorf("Cannot put object %s (%v) in devTable file %s, err %v",
				devArr[i], genVecArr[i], devfile, err)
		}
	}

	reclaimVec, err := dtab.computeReclaimVector()
	if err != nil {
		t.Fatalf("computeReclaimVector() failed devices: %v, vectors: %v in devTable file %s, err %v",
			devArr, genVecArr, devfile, err)
	}

	if !reflect.DeepEqual(reclaimVec, expReclaimVec) {
		t.Fatalf("Data mismatch for reclaimVec %v instead of %v",
			reclaimVec, expReclaimVec)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestAddDevice tests adding a device to the devTable.
func TestAddDevice(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	var dev DeviceID = "VeyronLaptop"
	if err := dtab.addDevice(dev); err != nil {
		t.Fatalf("Cannot add new device in devTable file %s, err %v", devfile, err)
	}

	vec, err := dtab.getGenVec(dev)
	if err != nil || vec == nil {
		t.Fatalf("GetGenVec() can not find object %s in devTable file %s, err %v",
			dev, devfile, err)
	}
	expVec := GenVector{dev: 0}
	if !reflect.DeepEqual(vec, expVec) {
		t.Errorf("Data mismatch for object %s in devTable file %s: %v instead of %v",
			dev, devfile, vec, expVec)
	}

	vec, err = dtab.getGenVec(dtab.s.id)
	if err != nil || vec == nil {
		t.Fatalf("GetGenVec() can not find object %s in devTable file %s, err %v",
			dtab.s.id, devfile, err)
	}
	expVec = GenVector{dtab.s.id: 0, dev: 0}
	if !reflect.DeepEqual(vec, expVec) {
		t.Errorf("Data mismatch for object %s in devTable file %s: %v instead of %v",
			dtab.s.id, devfile, vec, expVec)
	}

	expVec = GenVector{dtab.s.id: 10, "VeyronDesktop": 40, dev: 80}
	if err := dtab.putGenVec(dtab.s.id, expVec); err != nil {
		t.Fatalf("PutGenVec() can not put object %s in devTable file %s, err %v",
			dtab.s.id, devfile, err)
	}
	dev = "VeyronTab"
	if err := dtab.addDevice(dev); err != nil {
		t.Fatalf("Cannot add new device in devTable file %s, err %v", devfile, err)
	}
	expVec[dev] = 0

	vec, err = dtab.getGenVec(dtab.s.id)
	if err != nil || vec == nil {
		t.Fatalf("GetGenVec() can not find object %s in devTable file %s, err %v",
			dtab.s.id, devfile, err)
	}
	if !reflect.DeepEqual(vec, expVec) {
		t.Errorf("Data mismatch for object %s in devTable file %s: %v instead of %v",
			dtab.s.id, devfile, vec, expVec)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestUpdateReclaimVec tests updating the reclaim vector.
func TestUpdateReclaimVec(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	minGens := GenVector{"VeyronTab": 1, "VeyronDesktop": 3, "VeyronLaptop": 4}
	if err := dtab.updateReclaimVec(minGens); err != nil {
		t.Fatalf("Cannot update reclaimvec in devTable file %s, err %v", devfile, err)
	}
	expVec := GenVector{dtab.s.id: 0, "VeyronTab": 0, "VeyronDesktop": 2, "VeyronLaptop": 3}
	if !reflect.DeepEqual(dtab.head.ReclaimVec, expVec) {
		t.Errorf("Data mismatch for reclaimVec in devTable file %s: %v instead of %v",
			devfile, dtab.head.ReclaimVec, expVec)
	}

	dtab.head.ReclaimVec[DeviceID("VeyronTab")] = 4
	minGens = GenVector{"VeyronTab": 1}
	if err := dtab.updateReclaimVec(minGens); err == nil {
		t.Fatalf("Update reclaimvec didn't fail in devTable file %s", devfile)
	}

	minGens = GenVector{"VeyronTab": 5}
	if err := dtab.updateReclaimVec(minGens); err != nil {
		t.Fatalf("Cannot update reclaimvec in devTable file %s, err %v", devfile, err)
	}
	expVec = GenVector{dtab.s.id: 0, "VeyronTab": 4, "VeyronDesktop": 2, "VeyronLaptop": 3}
	if !reflect.DeepEqual(dtab.head.ReclaimVec, expVec) {
		t.Errorf("Data mismatch for reclaimVec in devTable file %s: %v instead of %v",
			devfile, dtab.head.ReclaimVec, expVec)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}

// TestDevTableCompact tests compacting of devtable's kvdb file.
func TestDevTableCompact(t *testing.T) {
	devfile := getFileName()
	defer os.Remove(devfile)

	s := &syncd{id: "VeyronPhone"}
	dtab, err := openDevTable(devfile, s)
	if err != nil {
		t.Fatalf("Cannot open new devTable file %s, err %v", devfile, err)
	}

	// Put some data in "devices" table.
	devices := []DeviceID{"Phone", "Tab", "Laptop", "Desktop"}
	for i, dev := range devices {
		v := GenVector{}
		for j, d := range devices {
			v[d] = GenID(i*100 + j)
		}
		if err := dtab.putGenVec(dev, v); err != nil {
			t.Errorf("Cannot put object %s (%v) in devTable file %s, err %v", dev, v, devfile, err)
		}
		if err := dtab.flush(); err != nil {
			t.Errorf("Cannot flush devTable file %s, err %v", devfile, err)
		}
	}

	// Put some data in "header" table.
	expMark := []byte{1, 2, 3}
	expVec := GenVector{
		"VeyronTab":   30,
		"VeyronPhone": 10,
	}
	dtab.head = &devTableHeader{
		Resmark:    expMark,
		ReclaimVec: expVec,
	}
	if err := dtab.flush(); err != nil {
		t.Errorf("Cannot flush devTable file %s, err %v", devfile, err)
	}

	// Get size before compaction.
	oldSize := getFileSize(devfile)
	if oldSize < 0 {
		t.Fatalf("DevTable file %s not created", devfile)
	}

	if err := dtab.compact(); err != nil {
		t.Errorf("Cannot compact devTable file %s, err %v", devfile, err)
	}

	// Verify size of kvdb file is reduced.
	size := getFileSize(devfile)
	if size < 0 {
		t.Fatalf("DevTable file %s not created", devfile)
	}
	if size > oldSize {
		t.Fatalf("DevTable file %s not compacted", devfile)
	}

	// Check data exists after compaction.
	for i, dev := range devices {
		exp := GenVector{}
		for j, d := range devices {
			exp[d] = GenID(i*100 + j)
		}
		curVec, err := dtab.getGenVec(dev)
		if err != nil || curVec == nil {
			t.Fatalf("GetGenVec() can not find object %s in devTable file %s, err %v",
				dev, devfile, err)
		}
		if !reflect.DeepEqual(exp, curVec) {
			t.Errorf("Data mismatch for object %s in devTable file %s: %v instead of %v",
				dev, devfile, curVec, exp)
		}
	}

	dtab.head.Resmark = nil
	dtab.head.ReclaimVec = GenVector{}
	if err := dtab.getHead(); err != nil {
		t.Fatalf("getHead() can not find head in devTable file %s, err %v", devfile, err)
	}
	if !reflect.DeepEqual(dtab.head.Resmark, expMark) {
		t.Errorf("Data mismatch for resmark in devTable file %s: %v instead of %v",
			devfile, dtab.head.Resmark, expMark)
	}
	if !reflect.DeepEqual(dtab.head.ReclaimVec, expVec) {
		t.Errorf("Data mismatch for reclaimVec in devTable file %s: %v instead of %v",
			devfile, dtab.head.ReclaimVec, expVec)
	}

	if err := dtab.close(); err != nil {
		t.Errorf("Cannot close devTable file %s, err %v", devfile, err)
	}
}
