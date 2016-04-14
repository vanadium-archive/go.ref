// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"fmt"
	"math/rand"
	"time"

	"v.io/v23/context"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Writer is a Client that periodically writes key/value pairs to a set of
// collections in a database.  The time between writes can be configured by
// setting WriteInterval.  The key/value pairs can be configured by setting
// KeyValueFunc.
type Writer struct {
	// Amount of time to wait between db writes.  Defaults to 1 second.
	WriteInterval time.Duration

	// Function that generates successive key/value pairs.  If nil, keys will
	// be strings containing the current unix timestamp and values will be
	// random hex strings.
	KeyValueFunc func(time.Time) (string, interface{})

	ctx         *context.T
	db          syncbase.Database
	collections []syncbase.Collection
}

var _ Client = (*Writer)(nil)

func defaultKeyValueFunc(t time.Time) (string, interface{}) {
	return string(t.Unix()), fmt.Sprintf("%08x", rand.Int31())
}

func (w *Writer) String() string {
	s := "Writer"
	if w.db != nil {
		s += "-" + w.db.FullName()
	}
	return s
}

func (w *Writer) onTick(t time.Time) error {
	key, value := w.KeyValueFunc(t)
	for _, col := range w.collections {
		if err := col.Put(w.ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) Run(ctx *context.T, sbName, dbName string, collectionIds []wire.Id, stopChan <-chan struct{}) error {
	w.ctx = ctx
	var err error
	w.db, w.collections, err = createDbAndCollections(ctx, sbName, dbName, collectionIds)
	if err != nil {
		return err
	}

	interval := w.WriteInterval
	if interval == 0 {
		interval = 1 * time.Second
	}

	if w.KeyValueFunc == nil {
		w.KeyValueFunc = defaultKeyValueFunc
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case t := <-ticker.C:
			if err := w.onTick(t); err != nil {
				return err
			}
		case <-stopChan:
			return nil
		}
	}
}
