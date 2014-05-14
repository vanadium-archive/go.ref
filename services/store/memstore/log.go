package memstore

// A log consist of:
//    1. A header.
//    2. A state snapshot.
//    3. A sequence of transactions.
//
// The header includes information about the log version, the size of the state
// snapshot, the root storage.ID, etc.  The snapshot is a sequence of entries for
// each of the values in the state.  A transaction is a *mutations object.
//
// There are separate interfaces for reading writing; *wlog is used for writing,
// and *rlog is used for reading.
import (
	"errors"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"veyron/runtimes/google/lib/follow"
	"veyron/services/store/memstore/state"

	"veyron2/security"
	"veyron2/vom"
)

const (
	logfileName = "storage.log"
	// TODO(tilaks): determine correct permissions for the logs.
	dirPerm  = os.FileMode(0700)
	filePerm = os.FileMode(0600)
)

var (
	errLogIsClosed = errors.New("log is closed")
)

// wlog is the type of log writers.
type wlog struct {
	file *os.File
	enc  *vom.Encoder
}

// RLog is the type of log readers.
type RLog struct {
	mu     sync.Mutex
	closed bool // GUARDED_BY(mu)
	reader io.ReadCloser
	dec    *vom.Decoder
}

// createLog creates a log writer. dbName is the path of the database directory,
// to which transaction logs are written.
func createLog(dbName string) (*wlog, error) {
	// Create the log file at the default path in the database directory.
	filePath := path.Join(dbName, logfileName)
	if err := os.MkdirAll(dbName, dirPerm); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return nil, err
	}
	return &wlog{
		file: file,
		enc:  vom.NewEncoder(file),
	}, nil
}

// close closes the log file.
func (l *wlog) close() {
	l.file.Close()
	l.file = nil
	l.enc = nil
}

// writeState writes the initial state.
func (l *wlog) writeState(st *Store) error {
	// If writeState returns a nil error, the caller should assume that
	// future reads from the log file will discover the new state.
	// Therefore we commit the log file's new content to stable storage.
	// Note: l.enc does not buffer output, and doesn't need to be flushed.
	if l.file == nil {
		return errLogIsClosed
	}
	defer l.file.Sync()
	return st.State.Write(l.enc)
}

// appendTransaction adds a transaction to the end of the log.
func (l *wlog) appendTransaction(m *state.Mutations) error {
	// If appendTransaction returns a nil error, the caller should assume that
	// future reads from the log file will discover the new transaction.
	// Therefore we commit the log file's new content to stable storage.
	// Note: l.enc does not buffer output, and doesn't need to be flushed.
	if l.file == nil {
		return errLogIsClosed
	}
	defer l.file.Sync()
	return l.enc.Encode(m)
}

// OpenLog opens a log for reading. dbName is the path of the database directory.
// If followLog is true, reads block until records can be read. Otherwise,
// reads return EOF when no record can be read.
func OpenLog(dbName string, followLog bool) (*RLog, error) {
	// Open the log file at the default path in the database directory.
	filePath := path.Join(dbName, logfileName)
	var reader io.ReadCloser
	var err error
	if followLog {
		reader, err = follow.NewReader(filePath)
	} else {
		reader, err = os.Open(filePath)
	}
	if err != nil {
		return nil, err
	}
	return &RLog{
		reader: reader,
		dec:    vom.NewDecoder(reader),
	}, nil
}

// Close closes the log. If Close is called concurrently with ReadState or
// ReadTransaction, ongoing reads will terminate. Close is idempotent.
func (l *RLog) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	l.reader.Close()
}

func (l *RLog) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

// ReadState reads the initial state state. ReadState returns an error if the
// log is closed before or during the read. ReadState should not be invoked
// concurrently with other reads.
func (l *RLog) ReadState(adminID security.PublicID) (*Store, error) {
	if l.isClosed() {
		return nil, errLogIsClosed
	}

	// Create the state struct.
	st, err := New(adminID, "")
	if err != nil {
		return nil, err
	}

	// Create the state without refcounts.
	if err := st.State.Read(l.dec); err != nil {
		return nil, err
	}

	return st, nil
}

// ReadTransaction reads a transaction entry from the log. ReadTransaction
// returns an error if the log is closed before or during the read.
// ReadTransaction should not be invoked concurrently with other reads.
func (l *RLog) ReadTransaction() (*state.Mutations, error) {
	if l.isClosed() {
		return nil, errLogIsClosed
	}

	var ms state.Mutations
	if err := l.dec.Decode(&ms); err != nil {
		return nil, err
	}
	for _, m := range ms.Delta {
		m.UpdateRefs()
	}
	return &ms, nil
}

// backup the log file.
func backupLog(dbName string) error {
	srcPath := path.Join(dbName, logfileName)
	dstPath := srcPath + "." + time.Now().Format(time.RFC3339)
	return os.Rename(srcPath, dstPath)
}

// openDB opens the log file if it exists. dbName is the path of the database
// directory. If followLog is true, reads block until records can be read.
// Otherwise, reads return EOF when no record can be read.
func openDB(dbName string, followLog bool) (*RLog, error) {
	if dbName == "" {
		return nil, nil
	}
	rlog, err := OpenLog(dbName, followLog)
	if err != nil && os.IsNotExist(err) {
		// It is not an error for the log not to exist.
		err = nil
	}
	return rlog, err
}

// readAndCloseDB reads the state from the log file.
func readAndCloseDB(admin security.PublicID, rlog *RLog) (*Store, error) {
	defer rlog.Close()
	st, err := rlog.ReadState(admin)
	if err != nil {
		return nil, err
	}
	for {
		mu, err := rlog.ReadTransaction()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := st.ApplyMutations(mu); err != nil {
			return nil, err
		}
	}
	return st, nil
}
