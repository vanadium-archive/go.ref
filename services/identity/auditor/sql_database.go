package auditor

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"

	"time"
	"veyron.io/veyron/veyron2/vlog"
)

// SQLConfig contains the information to create a connection to a sql database.
type SQLConfig struct {
	// Database is a driver specific string specifying how to connect to the database.
	Database string `json:"database"`
	Table    string `json:"table"`
}

type database interface {
	Insert(entry databaseEntry) error
	Query(email string) <-chan databaseEntry
}

type databaseEntry struct {
	email, revocationCaveatID string
	caveats, blessings        []byte
	timestamp                 time.Time
	decodeErr                 error
}

// newSQLDatabase returns a SQL implementation of the database interface.
// If the table does not exist it creates it.
func newSQLDatabase(config SQLConfig) (database, error) {
	db, err := sql.Open("mysql", config.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to create database with config(%v): %v", config, err)
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	createStmt, err := db.Prepare(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s ( Email NVARCHAR(256), Caveats BLOB, Timestamp DATETIME, RevocationCaveatID NVARCHAR(1000), Blessings BLOB );", config.Table))
	if err != nil {
		return nil, err
	}
	if _, err = createStmt.Exec(); err != nil {
		return nil, err
	}
	insertStmt, err := db.Prepare(fmt.Sprintf("INSERT INTO %s (Email, Caveats, RevocationCaveatID, Timestamp, Blessings) VALUES (?, ?, ?, ?, ?)", config.Table))
	if err != nil {
		return nil, err
	}
	queryStmt, err := db.Prepare(fmt.Sprintf("SELECT Email, Caveats, RevocationCaveatID, Timestamp, Blessings from %s WHERE Email=?", config.Table))
	return sqlDatabase{insertStmt, queryStmt}, err
}

type sqlDatabase struct {
	insertStmt, queryStmt *sql.Stmt
}

func (s sqlDatabase) Insert(entry databaseEntry) error {
	_, err := s.insertStmt.Exec(entry.email, entry.caveats, entry.revocationCaveatID, entry.timestamp, entry.blessings)
	return err
}

func (s sqlDatabase) Query(email string) <-chan databaseEntry {
	c := make(chan databaseEntry)
	go s.sendDatabaseEntries(email, c)
	return c
}

func (s sqlDatabase) sendDatabaseEntries(email string, dst chan<- databaseEntry) {
	defer close(dst)
	rows, err := s.queryStmt.Query(email)
	if err != nil {
		vlog.Errorf("query failed %v", err)
		dst <- databaseEntry{decodeErr: fmt.Errorf("Failed to query for all audits: %v", err)}
		return
	}
	for rows.Next() {
		var dbentry databaseEntry
		if err = rows.Scan(&dbentry.email, &dbentry.caveats, &dbentry.revocationCaveatID, &dbentry.timestamp, &dbentry.blessings); err != nil {
			vlog.Errorf("scan of row failed %v", err)
			dbentry.decodeErr = fmt.Errorf("failed to read sql row, %s", err)
		}
		dst <- dbentry
	}
}
