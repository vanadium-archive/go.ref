// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"bytes"
	"fmt"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
)

// TestWatchLogBatch tests fetching a batch of log records.
func TestWatchLogBatch(t *testing.T) {
	runTest(t, []string{util.RowPrefix, util.PermsPrefix}, runWatchLogBatchTest)
}

// runWatchLogBatchTest tests fetching a batch of log records.
func runWatchLogBatchTest(t *testing.T, st store.Store) {
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
	resmark := MakeResumeMarker(0)
	var seq uint64

	for i := 0; i < (numTx + 3); i++ {
		logs, newResmark, err := WatchLogBatch(st, resmark)
		if err != nil {
			t.Fatalf("can't get watch log batch: %v", err)
		}
		if i < numTx {
			if len(logs) != numPut {
				t.Errorf("log fetch (i=%d) wrong log seq: %d instead of %d",
					i, len(logs), numPut)
			}

			seq += uint64(len(logs))
			expResmark := MakeResumeMarker(seq)
			if !bytes.Equal(newResmark, expResmark) {
				t.Errorf("log fetch (i=%d) wrong resmark: %s instead of %s",
					i, newResmark, expResmark)
			}

			for j, log := range logs {
				op := log.Op.(OpPut)
				expKey, expVal := makeKeyVal(i, j)
				key := op.Value.Key
				if !bytes.Equal(key, expKey) {
					t.Errorf("log fetch (i=%d, j=%d) bad key: %s instead of %s",
						i, j, key, expKey)
				}
				tx := st.NewTransaction()
				var val []byte
				val, err := GetAtVersion(nil, tx, key, val, op.Value.Version)
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
			if logs != nil || !bytes.Equal(newResmark, resmark) {
				t.Errorf("NOP log fetch (i=%d) had changes: %d logs, resmask %s",
					i, len(logs), newResmark)
			}
		}
		resmark = newResmark
	}
}
