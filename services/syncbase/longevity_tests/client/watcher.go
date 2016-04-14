// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"sync"

	"v.io/v23/context"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/services/watch"
	"v.io/v23/syncbase"
)

// Watcher is a client that watches a range of keys in a set of collections.
type Watcher struct {
	// Prefix to watch.  Defaults to empty string.
	// TODO(nlacasse): Allow different prefixes per collection?
	Prefix string

	// ResumeMarker to begin watching from.
	// TODO(nlacasse): Allow different ResumeMarkers per collection?
	ResumeMarker watch.ResumeMarker

	// OnChange runs for each WatchChange.  Must be goroutine-safe.  By default
	// this will be a no-op.
	OnChange func(syncbase.WatchChange)

	ctx         *context.T
	db          syncbase.Database
	collections []syncbase.Collection
}

var _ Client = (*Watcher)(nil)

func (w *Watcher) String() string {
	s := "Watcher"
	if w.db != nil {
		s += "-" + w.db.FullName()
	}
	return s
}

func (w *Watcher) Run(ctx *context.T, sbName, dbName string, collectionIds []wire.Id, stopChan <-chan struct{}) error {
	w.ctx = ctx
	var err error
	w.db, w.collections, err = createDbAndCollections(ctx, sbName, dbName, collectionIds)
	if err != nil {
		return err
	}

	// Get a WatchStream for each collection and spawn a goroutine to wait for
	// changes.
	wg := sync.WaitGroup{}
	for _, col := range w.collections {
		wg.Add(1)
		ctxWithCancel, cancel := context.WithCancel(w.ctx)
		stream, err := w.db.Watch(ctxWithCancel, col.Id(), w.Prefix, w.ResumeMarker)
		if err != nil {
			return err
		}
		go func() {
			defer wg.Done()
			for {
				// Call Advance() in a goroutine and send its value on
				// advanceChan.
				advanceChan := make(chan bool)
				go func() {
					advanceChan <- stream.Advance()
				}()

				// Wait on advance and stop channels.
				select {
				case advance := <-advanceChan:
					if !advance {
						// Stream ended.
						return
					}
					change := stream.Change()
					if w.OnChange != nil {
						w.OnChange(change)
					}
				case <-stopChan:
					// TODO(nlacasse): We should call stream.Cancel() here in
					// order to stop the Advance call in the goroutine.
					// However, due to  bug https://v.io/i/1284, calling Cancel
					// while Advance is running will lead to a deadlock.
					// As a workaround, we cancel the context, rather than the
					// stream.  Once the above bug is resolved, we should call
					// stream.Cancel() here.
					cancel()
					return
				}
			}
		}()
	}

	wg.Wait()
	return nil
}
