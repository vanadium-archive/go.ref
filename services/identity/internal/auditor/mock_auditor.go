// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auditor

import (
	"reflect"
	"v.io/x/ref/security/audit"
)

func NewMockBlessingAuditor() (audit.Auditor, BlessingLogReader) {
	db := &mockDatabase{}
	return &blessingAuditor{db}, &blessingLogReader{db}
}

type mockDatabase struct {
	NextEntry databaseEntry
}

func (db *mockDatabase) Insert(entry databaseEntry) error {
	db.NextEntry = entry
	return nil
}

func (db *mockDatabase) Query(email string) <-chan databaseEntry {
	c := make(chan databaseEntry)
	go func() {
		var empty databaseEntry
		if !reflect.DeepEqual(db.NextEntry, empty) {
			c <- db.NextEntry
		}
		close(c)
	}()
	return c
}
