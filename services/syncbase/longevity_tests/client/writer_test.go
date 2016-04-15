// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"testing"
	"time"

	"v.io/v23"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/longevity_tests/client"
	"v.io/x/ref/services/syncbase/testutil"
)

func TestWriter(t *testing.T) {
	ctx, sbName, cleanup := testutil.SetupOrDie(nil)
	defer cleanup()

	var writeCounter int32
	wroteKeys := []string{}
	wroteVals := []interface{}{}
	keyValueFunc := func(_ time.Time) (string, interface{}) {
		key := string(writeCounter)
		wroteKeys = append(wroteKeys, key)
		value := writeCounter
		wroteVals = append(wroteVals, value)
		writeCounter++
		return key, value
	}
	writer := &client.Writer{
		WriteInterval: 50 * time.Millisecond,
		KeyValueFunc:  keyValueFunc,
	}

	blessing, _ := v23.GetPrincipal(ctx).BlessingStore().Default()
	blessingString := blessing.String()

	stop := make(chan struct{})
	dbName := "test_db"
	collectionIds := []wire.Id{
		wire.Id{
			Blessing: blessingString,
			Name:     "test_col_1",
		},
		wire.Id{
			Blessing: blessingString,
			Name:     "test_col_2",
		},
	}

	// Run the writer for 2 seconds.
	var err error
	go func() {
		err = writer.Run(ctx, sbName, dbName, collectionIds, stop)
	}()
	time.Sleep(2 * time.Second)
	close(stop)
	if err != nil {
		t.Fatalf("writer error: %v", err)
	}

	// Make sure database exists.
	service := syncbase.NewService(sbName)
	db := service.Database(ctx, dbName, nil)
	if exists, err := db.Exists(ctx); !exists || err != nil {
		t.Fatalf("expected db.Exists() to return true, nil but got: %v, %v", exists, err)
	}

	// Make sure collections exist.
	for _, id := range collectionIds {
		if exists, err := db.CollectionForId(id).Exists(ctx); !exists || err != nil {
			t.Fatalf("expected collection.Exists() to return true, nil but got: %v, %v", exists, err)
		}
	}

	// Make sure we did at least 2 writes.
	if writeCounter < 2 {
		t.Errorf("expected to write at least 2 rows, but only wrote %d", writeCounter)
	}

	// Check that each collection contains the wroteKeys and wroteVals.
	for _, id := range collectionIds {
		col := db.CollectionForId(id)
		testutil.CheckScan(t, ctx, col, syncbase.Range("", string(writeCounter)), wroteKeys, wroteVals)
	}
}
