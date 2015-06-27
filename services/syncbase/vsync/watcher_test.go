// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for the sync watcher in Syncbase.

import (
	"reflect"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/server/watchable"
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
			Op: &watchable.OpPut{
				watchable.PutOp{Key: []byte(test.key)},
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
		log.Op = &watchable.OpDelete{watchable.DeleteOp{Key: k}}
	} else {
		log.Op = &watchable.OpPut{watchable.PutOp{Key: k, Version: v}}
	}
	return log
}

// newSGLog creates a SyncGroup watch log entry.
func newSGLog(prefixes []string, remove bool) *watchable.LogEntry {
	return &watchable.LogEntry{
		Op: &watchable.OpSyncGroup{
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

	// Empty logs does not fail.
	s.processWatchLogBatch(nil, app, db, st, nil, "")

	// Non-syncable logs.
	batch := []*watchable.LogEntry{
		newLog("foo", "123", false),
		newLog("bar", "555", false),
	}

	resmark := "abcd"
	s.processWatchLogBatch(nil, app, db, st, batch, resmark)

	if res, err := getResMark(nil, st); err != nil && res != resmark {
		t.Errorf("invalid resmark batch processing: got %s instead of %s", res, resmark)
	}
	if hasNode(nil, st, "foo", "123") || hasNode(nil, st, "bar", "555") {
		t.Error("hasNode() found DAG entries for non-syncable logs")
	}

	// Partially syncable logs.
	batch = []*watchable.LogEntry{
		newSGLog([]string{"f", "x"}, false),
		newLog("foo", "333", false),
		newLog("fooxyz", "444", false),
		newLog("bar", "222", false),
	}

	resmark = "cdef"
	s.processWatchLogBatch(nil, app, db, st, batch, resmark)

	if res, err := getResMark(nil, st); err != nil && res != resmark {
		t.Errorf("invalid resmark batch processing: got %s instead of %s", res, resmark)
	}
	if head, err := getHead(nil, st, "foo"); err != nil && head != "333" {
		t.Errorf("getHead() did not find foo: %s, %v", head, err)
	}
	node, err := getNode(nil, st, "foo", "333")
	if err != nil {
		t.Errorf("getNode() did not find foo: %v", err)
	}
	if node.Level != 0 || node.Parents != nil || node.Logrec == "" || node.BatchId == NoBatchId {
		t.Errorf("invalid DAG node for foo: %v", node)
	}
	node2, err := getNode(nil, st, "fooxyz", "444")
	if err != nil {
		t.Errorf("getNode() did not find fooxyz: %v", err)
	}
	if node2.Level != 0 || node2.Parents != nil || node2.Logrec == "" || node2.BatchId != node.BatchId {
		t.Errorf("invalid DAG node for fooxyz: %v", node2)
	}
	if hasNode(nil, st, "bar", "222") {
		t.Error("hasNode() found DAG entries for non-syncable logs")
	}

	// Back to non-syncable logs (remove "f" prefix).
	batch = []*watchable.LogEntry{
		newSGLog([]string{"f"}, true),
		newLog("foo", "99", false),
		newLog("fooxyz", "888", true),
		newLog("bar", "007", false),
	}

	resmark = "tuvw"
	s.processWatchLogBatch(nil, app, db, st, batch, resmark)

	if res, err := getResMark(nil, st); err != nil && res != resmark {
		t.Errorf("invalid resmark batch processing: got %s instead of %s", res, resmark)
	}
	// No changes to "foo".
	if head, err := getHead(nil, st, "foo"); err != nil && head != "333" {
		t.Errorf("getHead() did not find foo: %s, %v", head, err)
	}
	if node, err := getNode(nil, st, "foo", "99"); err == nil {
		t.Errorf("getNode() should not have found foo @ 99: %v", node)
	}
	if node, err := getNode(nil, st, "fooxyz", "888"); err == nil {
		t.Errorf("getNode() should not have found fooxyz @ 888: %v", node)
	}
	if hasNode(nil, st, "bar", "007") {
		t.Error("hasNode() found DAG entries for non-syncable logs")
	}
}
