package auditor

import (
	"reflect"
	"v.io/core/veyron/security/audit"
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
