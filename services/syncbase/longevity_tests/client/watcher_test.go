// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"v.io/v23"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/longevity_tests/client"
	"v.io/x/ref/services/syncbase/testutil"
)

func TestWatcher(t *testing.T) {
	ctx, sbName, cleanup := testutil.SetupOrDie(nil)
	defer cleanup()

	// gotRows records the rows in the watchChanges that the watcher receives.
	// Since OnChange is called in a goroutine, this must be locked by a mutex.
	gotRows, mu := []string{}, sync.Mutex{}
	// wg is used to stop the test when watcher.OnChange has fired for each of
	// the rows we put.
	wg := sync.WaitGroup{}
	watcher := &client.Watcher{
		OnChange: func(watchChange syncbase.WatchChange) {
			mu.Lock()
			defer mu.Unlock()
			gotRows = append(gotRows, watchChange.Row)
			wg.Done()
		},
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

	// Create the database and each collection.
	// The watcher will attempt to do this as well, but doing it ourselves
	// avoids a race condition in the tests.
	db := testutil.CreateDatabase(t, ctx, syncbase.NewService(sbName), dbName)
	collections := make([]syncbase.Collection, len(collectionIds))
	for i, id := range collectionIds {
		col := db.CollectionForId(id)
		if err := col.Create(ctx, testutil.DefaultPerms(blessingString)); err != nil {
			t.Fatal(err)
		}
		collections[i] = col
	}

	// Run the watcher in a goroutine.
	wErr := make(chan error)
	go func() {
		wErr <- watcher.Run(ctx, sbName, dbName, collectionIds, stop)
	}()

	// Put 3 rows to each collection.
	wantRows := []string{}
	for i, col := range collections {
		for j := 0; j < 3; j++ {
			wg.Add(1)
			key := fmt.Sprintf("key-%d-%d", i, j)
			val := fmt.Sprintf("val-%d-%d", i, j)
			if err := col.Put(ctx, key, val); err != nil {
				t.Fatal(err)
			}
			wantRows = append(wantRows, key)
		}
	}

	// Wait for watcher to receive all the rows we put.
	wg.Wait()

	// Stop the watcher.
	close(stop)

	if err := <-wErr; err != nil {
		t.Errorf("watcher.Run returned error: %v", err)
	}

	// Check that we got watchChanges for each row that we put to.
	sort.Strings(wantRows)
	sort.Strings(gotRows)
	if !reflect.DeepEqual(wantRows, gotRows) {
		fmt.Errorf("wanted gotRows to be %v but got %v", wantRows, gotRows)
	}
}
