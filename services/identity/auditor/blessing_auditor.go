package auditor

import (
	"bytes"
	"database/sql"
	"fmt"
	"strings"
	"time"

	vsecurity "v.io/core/veyron/security"
	"v.io/core/veyron/security/audit"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vom"
)

// BlessingLogReader provides the Read method to read audit logs.
// Read returns a channel of BlessingEntrys whose extension matches the provided email.
type BlessingLogReader interface {
	Read(email string) <-chan BlessingEntry
}

// BlessingEntry contains important logged information about a blessed principal.
type BlessingEntry struct {
	Email              string
	Caveats            []security.Caveat
	Timestamp          time.Time // Time when the blesings were created.
	RevocationCaveatID string
	Blessings          security.Blessings
	DecodeError        error
}

// NewSQLBlessingAuditor returns an auditor for wrapping a principal with, and a BlessingLogReader
// for reading the audits made by that auditor. The config is used to construct the connection
// to the SQL database that the auditor and BlessingLogReader use.
func NewSQLBlessingAuditor(sqlDB *sql.DB) (audit.Auditor, BlessingLogReader, error) {
	db, err := newSQLDatabase(sqlDB, "BlessingAudit")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create sql db: %v", err)
	}
	auditor, reader := &blessingAuditor{db}, &blessingLogReader{db}
	return auditor, reader, nil
}

type blessingAuditor struct {
	db database
}

func (a *blessingAuditor) Audit(entry audit.Entry) error {
	if entry.Method != "Bless" {
		return nil
	}
	dbentry, err := newDatabaseEntry(entry)
	if err != nil {
		return err
	}
	return a.db.Insert(dbentry)
}

type blessingLogReader struct {
	db database
}

func (r *blessingLogReader) Read(email string) <-chan BlessingEntry {
	c := make(chan BlessingEntry)
	go r.sendAuditEvents(c, email)
	return c
}

func (r *blessingLogReader) sendAuditEvents(dst chan<- BlessingEntry, email string) {
	defer close(dst)
	dbch := r.db.Query(email)
	for dbentry := range dbch {
		dst <- newBlessingEntry(dbentry)
	}
}

func newDatabaseEntry(entry audit.Entry) (databaseEntry, error) {
	d := databaseEntry{timestamp: entry.Timestamp}
	extension, ok := entry.Arguments[2].(string)
	if !ok {
		return d, fmt.Errorf("failed to extract extension")
	}
	d.email = strings.Split(extension, "/")[0]
	var caveats []security.Caveat
	for _, arg := range entry.Arguments[3:] {
		if cav, ok := arg.(security.Caveat); !ok {
			return d, fmt.Errorf("failed to extract Caveat")
		} else {
			caveats = append(caveats, cav)
		}
	}
	var blessings security.Blessings
	if blessings, ok = entry.Results[0].(security.Blessings); !ok {
		return d, fmt.Errorf("failed to extract result blessing")
	}
	{
		var buf bytes.Buffer
		if err := vom.NewEncoder(&buf).Encode(security.MarshalBlessings(blessings)); err != nil {
			return d, err
		}
		d.blessings = buf.Bytes()
	}
	{
		var buf bytes.Buffer
		if err := vom.NewEncoder(&buf).Encode(caveats); err != nil {
			return d, err
		}
		d.caveats = buf.Bytes()
	}
	return d, nil
}

func newBlessingEntry(dbentry databaseEntry) BlessingEntry {
	if dbentry.decodeErr != nil {
		return BlessingEntry{DecodeError: dbentry.decodeErr}
	}
	b := BlessingEntry{
		Email:     dbentry.email,
		Timestamp: dbentry.timestamp,
	}
	var wireBlessings security.WireBlessings
	var err error
	if err = vom.NewDecoder(bytes.NewBuffer(dbentry.blessings)).Decode(&wireBlessings); err != nil {
		return BlessingEntry{DecodeError: fmt.Errorf("failed to decode blessings: %s", err)}
	}
	if b.Blessings, err = security.NewBlessings(wireBlessings); err != nil {
		return BlessingEntry{DecodeError: fmt.Errorf("failed to construct blessings: %s", err)}
	}
	if err = vom.NewDecoder(bytes.NewBuffer(dbentry.caveats)).Decode(&b.Caveats); err != nil {
		return BlessingEntry{DecodeError: fmt.Errorf("failed to decode caveats: %s", err)}
	}
	b.RevocationCaveatID = revocationCaveatID(b.Caveats)
	return b
}

func revocationCaveatID(caveats []security.Caveat) string {
	for _, tpcav := range vsecurity.ThirdPartyCaveats(caveats...) {
		return tpcav.ID()
	}
	return ""
}
