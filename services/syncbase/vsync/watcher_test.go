// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for the sync watcher in Syncbase.

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/server/watchable"
	"v.io/v23/vom"
	_ "v.io/x/ref/runtime/factories/generic"
)

// TestSetResmark tests setting and getting a resume marker.
func TestSetResmark(t *testing.T) {
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()

	resmark, err := getResMark(nil, st)
	if err == nil || resmark != "" {
		t.Errorf("found non-existent resume marker: %s, %v", resmark, err)
	}

	wantResmark := "1234567890"
	tx := st.NewTransaction()
	if err := setResMark(nil, tx, wantResmark); err != nil {
		t.Errorf("cannot set resume marker: %v", err)
	}
	tx.Commit()

	resmark, err = getResMark(nil, st)
	if err != nil {
		t.Errorf("cannot get new resume marker: %v", err)
	}
	if resmark != wantResmark {
		t.Errorf("invalid new resume: got %s instead of %s", resmark, wantResmark)
	}
}

// TestWatchPrefixes tests setting and updating the watch prefixes map.
func TestWatchPrefixes(t *testing.T) {
	watchPollInterval = time.Millisecond
	svc := createService(t)
	defer destroyService(t, svc)

	if len(watchPrefixes) != 0 {
		t.Errorf("watch prefixes not empty: %v", watchPrefixes)
	}

	watchPrefixOps := []struct {
		appName, dbName, key string
		incr                 bool
	}{
		{"app1", "db1", "foo", true},
		{"app1", "db1", "bar", true},
		{"app2", "db1", "xyz", true},
		{"app3", "db1", "haha", true},
		{"app1", "db1", "foo", true},
		{"app1", "db1", "foo", true},
		{"app1", "db1", "foo", false},
		{"app2", "db1", "ttt", true},
		{"app2", "db1", "ttt", true},
		{"app2", "db1", "ttt", false},
		{"app2", "db1", "ttt", false},
		{"app2", "db2", "qwerty", true},
		{"app3", "db1", "haha", true},
		{"app2", "db2", "qwerty", false},
		{"app3", "db1", "haha", false},
	}

	for _, op := range watchPrefixOps {
		if op.incr {
			incrWatchPrefix(op.appName, op.dbName, op.key)
		} else {
			decrWatchPrefix(op.appName, op.dbName, op.key)
		}
	}

	expPrefixes := map[string]sgPrefixes{
		"app1:db1": sgPrefixes{"foo": 2, "bar": 1},
		"app2:db1": sgPrefixes{"xyz": 1},
		"app3:db1": sgPrefixes{"haha": 1},
	}
	if !reflect.DeepEqual(watchPrefixes, expPrefixes) {
		t.Errorf("invalid watch prefixes: got %v instead of %v", watchPrefixes, expPrefixes)
	}

	checkSyncableTests := []struct {
		appName, dbName, key string
		result               bool
	}{
		{"app1", "db1", "foo", true},
		{"app1", "db1", "foobar", true},
		{"app1", "db1", "bar", true},
		{"app1", "db1", "bar123", true},
		{"app1", "db1", "f", false},
		{"app1", "db1", "ba", false},
		{"app1", "db1", "xyz", false},
		{"app1", "db555", "foo", false},
		{"app555", "db1", "foo", false},
		{"app2", "db1", "xyz123", true},
		{"app2", "db1", "ttt123", false},
		{"app2", "db2", "qwerty", false},
		{"app3", "db1", "hahahoho", true},
		{"app3", "db1", "hoho", false},
		{"app3", "db1", "h", false},
	}

	for _, test := range checkSyncableTests {
		log := &watchable.LogEntry{
			Op: watchable.OpPut{
				watchable.PutOp{Key: []byte(makeRowKey(test.key))},
			},
		}
		res := syncable(appDbName(test.appName, test.dbName), log)
		if res != test.result {
			t.Errorf("checkSyncable: invalid output: %s, %s, %s: got %t instead of %t",
				test.appName, test.dbName, test.key, res, test.result)
		}
	}
}

// newLog creates a Put or Delete watch log entry.
func newLog(key, version string, delete bool) *watchable.LogEntry {
	k, v := []byte(key), []byte(version)
	log := &watchable.LogEntry{}
	if delete {
		log.Op = watchable.OpDelete{watchable.DeleteOp{Key: k}}
	} else {
		log.Op = watchable.OpPut{watchable.PutOp{Key: k, Version: v}}
	}
	return log
}

// newSGLog creates a SyncGroup watch log entry.
func newSGLog(prefixes []string, remove bool) *watchable.LogEntry {
	return &watchable.LogEntry{
		Op: watchable.OpSyncGroup{
			watchable.SyncGroupOp{Prefixes: prefixes, Remove: remove},
		},
	}
}

// TestProcessWatchLogBatch tests the processing of a batch of log records.
func TestProcessWatchLogBatch(t *testing.T) {
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()
	s := svc.sync

	app, db := "mockapp", "mockdb"
	fooKey := makeRowKey("foo")
	barKey := makeRowKey("bar")
	fooxyzKey := makeRowKey("fooxyz")

	// Empty logs does not fail.
	s.processWatchLogBatch(nil, app, db, st, nil, "")

	// Non-syncable logs.
	batch := []*watchable.LogEntry{
		newLog(fooKey, "123", false),
		newLog(barKey, "555", false),
	}

	resmark := "abcd"
	s.processWatchLogBatch(nil, app, db, st, batch, resmark)

	if res, err := getResMark(nil, st); err != nil && res != resmark {
		t.Errorf("invalid resmark batch processing: got %s instead of %s", res, resmark)
	}
	if ok, err := hasNode(nil, st, fooKey, "123"); err != nil || ok {
		t.Error("hasNode() found DAG entry for non-syncable log on foo")
	}
	if ok, err := hasNode(nil, st, barKey, "555"); err != nil || ok {
		t.Error("hasNode() found DAG entry for non-syncable log on bar")
	}

	// Partially syncable logs.
	batch = []*watchable.LogEntry{
		newSGLog([]string{"f", "x"}, false),
		newLog(fooKey, "333", false),
		newLog(fooxyzKey, "444", false),
		newLog(barKey, "222", false),
	}

	resmark = "cdef"
	s.processWatchLogBatch(nil, app, db, st, batch, resmark)

	if res, err := getResMark(nil, st); err != nil && res != resmark {
		t.Errorf("invalid resmark batch processing: got %s instead of %s", res, resmark)
	}
	if head, err := getHead(nil, st, fooKey); err != nil && head != "333" {
		t.Errorf("getHead() did not find foo: %s, %v", head, err)
	}
	node, err := getNode(nil, st, fooKey, "333")
	if err != nil {
		t.Errorf("getNode() did not find foo: %v", err)
	}
	if node.Level != 0 || node.Parents != nil || node.Logrec == "" || node.BatchId != NoBatchId {
		t.Errorf("invalid DAG node for foo: %v", node)
	}
	node2, err := getNode(nil, st, fooxyzKey, "444")
	if err != nil {
		t.Errorf("getNode() did not find fooxyz: %v", err)
	}
	if node2.Level != 0 || node2.Parents != nil || node2.Logrec == "" || node2.BatchId != NoBatchId {
		t.Errorf("invalid DAG node for fooxyz: %v", node2)
	}
	if ok, err := hasNode(nil, st, barKey, "222"); err != nil || ok {
		t.Error("hasNode() found DAG entry for non-syncable log on bar")
	}

	// More partially syncable logs updating existing ones.
	batch = []*watchable.LogEntry{
		newLog(fooKey, "1", false),
		newLog(fooxyzKey, "", true),
		newLog(barKey, "7", false),
	}

	resmark = "ghij"
	s.processWatchLogBatch(nil, app, db, st, batch, resmark)

	if res, err := getResMark(nil, st); err != nil && res != resmark {
		t.Errorf("invalid resmark batch processing: got %s instead of %s", res, resmark)
	}
	if head, err := getHead(nil, st, fooKey); err != nil && head != "1" {
		t.Errorf("getHead() did not find foo: %s, %v", head, err)
	}
	node, err = getNode(nil, st, fooKey, "1")
	if err != nil {
		t.Errorf("getNode() did not find foo: %v", err)
	}
	expParents := []string{"333"}
	if node.Level != 1 || !reflect.DeepEqual(node.Parents, expParents) ||
		node.Logrec == "" || node.BatchId == NoBatchId {
		t.Errorf("invalid DAG node for foo: %v", node)
	}
	head2, err := getHead(nil, st, fooxyzKey)
	if err != nil {
		t.Errorf("getHead() did not find fooxyz: %v", err)
	}
	node2, err = getNode(nil, st, fooxyzKey, head2)
	if err != nil {
		t.Errorf("getNode() did not find fooxyz: %v", err)
	}
	expParents = []string{"444"}
	if node2.Level != 1 || !reflect.DeepEqual(node2.Parents, expParents) ||
		node2.Logrec == "" || node2.BatchId == NoBatchId {
		t.Errorf("invalid DAG node for fooxyz: %v", node2)
	}
	if ok, err := hasNode(nil, st, barKey, "7"); err != nil || ok {
		t.Error("hasNode() found DAG entry for non-syncable log on bar")
	}

	// Back to non-syncable logs (remove "f" prefix).
	batch = []*watchable.LogEntry{
		newSGLog([]string{"f"}, true),
		newLog(fooKey, "99", false),
		newLog(fooxyzKey, "888", true),
		newLog(barKey, "007", false),
	}

	resmark = "tuvw"
	s.processWatchLogBatch(nil, app, db, st, batch, resmark)

	if res, err := getResMark(nil, st); err != nil && res != resmark {
		t.Errorf("invalid resmark batch processing: got %s instead of %s", res, resmark)
	}
	// No changes to "foo".
	if head, err := getHead(nil, st, fooKey); err != nil && head != "333" {
		t.Errorf("getHead() did not find foo: %s, %v", head, err)
	}
	if node, err := getNode(nil, st, fooKey, "99"); err == nil {
		t.Errorf("getNode() should not have found foo @ 99: %v", node)
	}
	if node, err := getNode(nil, st, fooxyzKey, "888"); err == nil {
		t.Errorf("getNode() should not have found fooxyz @ 888: %v", node)
	}
	if ok, err := hasNode(nil, st, barKey, "007"); err != nil || ok {
		t.Error("hasNode() found DAG entry for non-syncable log on bar")
	}

	// Scan the batch records and verify that there is only 1 DAG batch
	// stored, with a total count of 3 and a map of 2 syncable entries.
	// This is because the 1st batch, while containing syncable keys, is a
	// SyncGroup snapshot that does not get assigned a batch ID.  The 2nd
	// batch is an application batch with 3 keys of which 2 are syncable.
	// The 3rd batch is also a SyncGroup snapshot.
	count := 0
	start, limit := util.ScanPrefixArgs(util.JoinKeyParts(util.SyncPrefix, "dag", "b"), "")
	stream := st.Scan(start, limit)
	for stream.Advance() {
		count++
		key := string(stream.Key(nil))
		var info batchInfo
		if err := vom.Decode(stream.Value(nil), &info); err != nil {
			t.Errorf("cannot decode batch %s: %v", key, err)
		}
		if info.Count != 3 {
			t.Errorf("wrong total count in batch %s: got %d instead of 3", key, info.Count)
		}
		if n := len(info.Objects); n != 2 {
			t.Errorf("wrong object count in batch %s: got %d instead of 2", key, n)
		}
	}
	if count != 1 {
		t.Errorf("wrong count of batches: got %d instead of 2", count)
	}
}

// TestGetWatchLogBatch tests fetching a batch of log records.
func TestGetWatchLogBatch(t *testing.T) {
	svc := createService(t)
	defer destroyService(t, svc)
	st := svc.St()

	// Create a set of batches to fill the log queue.
	numTx, numPut := 3, 4

	makeKeyVal := func(batchNum, recNum int) ([]byte, []byte) {
		key := util.JoinKeyParts(util.RowPrefix, fmt.Sprintf("foo-%d-%d", batchNum, recNum))
		val := fmt.Sprintf("val-%d-%d", batchNum, recNum)
		return []byte(key), []byte(val)
	}

	for i := 0; i < numTx; i++ {
		tx := st.NewTransaction()
		for j := 0; j < numPut; j++ {
			key, val := makeKeyVal(i, j)
			if err := tx.Put(key, val); err != nil {
				t.Errorf("cannot put %s (%s): %v", key, val, err)
			}
		}
		tx.Commit()
	}

	// Fetch the batches and a few more empty fetches and verify them.
	app, db := "mockapp", "mockdb"
	resmark := ""
	count := 0

	for i := 0; i < (numTx + 3); i++ {
		logs, newResmark := getWatchLogBatch(nil, app, db, st, resmark)
		if i < numTx {
			if len(logs) != numPut {
				t.Errorf("log fetch (i=%d) wrong log count: %d instead of %d",
					i, len(logs), numPut)
			}

			count += len(logs)
			expResmark := makeResMark(count - 1)
			if newResmark != expResmark {
				t.Errorf("log fetch (i=%d) wrong resmark: %s instead of %s",
					i, newResmark, expResmark)
			}

			for j, log := range logs {
				op := log.Op.(watchable.OpPut)
				expKey, expVal := makeKeyVal(i, j)
				key := op.Value.Key
				if !bytes.Equal(key, expKey) {
					t.Errorf("log fetch (i=%d, j=%d) bad key: %s instead of %s",
						i, j, key, expKey)
				}
				tx := st.NewTransaction()
				var val []byte
				val, err := watchable.GetAtVersion(nil, tx, key, val, op.Value.Version)
				if err != nil {
					t.Errorf("log fetch (i=%d, j=%d) cannot GetAtVersion(): %v", i, j, err)
				}
				if !bytes.Equal(val, expVal) {
					t.Errorf("log fetch (i=%d, j=%d) bad value: %s instead of %s",
						i, j, val, expVal)
				}
				tx.Abort()
			}
		} else {
			if logs != nil || newResmark != resmark {
				t.Errorf("NOP log fetch (i=%d) had changes: %d logs, resmask %s",
					i, len(logs), newResmark)
			}
		}
		resmark = newResmark
	}
}
