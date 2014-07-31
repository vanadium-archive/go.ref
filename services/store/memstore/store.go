package memstore

import (
	"veyron/runtimes/google/lib/sync"

	"veyron/services/store/memstore/state"
	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/verror"
)

// Store is the in-memory state of the store.
type Store struct {
	sync.DebugMutex

	// state holds the current state of the store.
	State *state.State

	// log persists the state to disk and any committed transactions.
	// An ephemeral state has a nil log, and does not persist to disk.
	log *wlog
}

var (
	ErrRequestCancelled = verror.Abortedf("request cancelled")
)

// New creates a new store.  admin is the public ID of the administrator, dbName
// is the path of the database directory, to which logs are written.
func New(admin security.PublicID, dbName string) (*Store, error) {
	rlog, err := openDB(dbName, false)
	if err != nil {
		return nil, err
	}
	if rlog == nil {
		return newState(admin, dbName)
	}
	st, err := readAndCloseDB(admin, rlog)
	if err != nil {
		return nil, err
	}
	// Rename the log file by date.
	if err := backupLog(dbName); err != nil {
		return nil, err
	}
	if err := st.setLog(dbName); err != nil {
		return nil, err
	}
	return st, nil
}

// newState returns an empty state. dbName is the path of the database directory,
// to which logs are written.
func newState(admin security.PublicID, dbName string) (*Store, error) {
	st := &Store{State: state.New(admin)}
	if err := st.setLog(dbName); err != nil {
		return nil, err
	}
	return st, nil
}

// setLog creates a fresh log file and writes to it.
func (st *Store) setLog(dbName string) error {
	if dbName != "" {
		log, err := createLog(dbName)
		if err != nil {
			return err
		}
		err = log.writeState(st)
		if err != nil {
			log.close()
			return err
		}
		st.log = log
	}
	return nil
}

// Bind returns an Object representing a value in the store.  The value need not
// exist; the Put method can be used to add the value if it doesn't already
// exist.
func (st *Store) Bind(path string) *Object {
	return &Object{path: storage.ParsePath(path), store: st}
}

func (st *Store) Close() error {
	st.Lock()
	st.closeLocked()
	st.Unlock()
	return nil
}

func (st *Store) closeLocked() {
	st.State = nil
	if st.log != nil {
		st.log.close()
	}
	st.log = nil
}

// GC performs a manual garbage collection.
func (st *Store) GC() error {
	st.Lock()
	defer st.Unlock()
	st.State.GC()

	// Append a transaction containing deletions, if there are any.
	mu := st.State.Deletions()
	if st.log != nil && mu != nil {
		if err := st.log.appendTransaction(mu); err != nil {
			// We can't continue because the log failed.  The state has already been updated,
			// but access to the state is blocked because we have the lock.  Close the state
			// to ensure that it is never used again.
			st.closeLocked()
			return err
		}
	}
	return nil
}

// Snapshot returns a read-only state.
func (st *Store) Snapshot() state.Snapshot {
	st.Lock()
	defer st.Unlock()
	return st.State.Snapshot()
}

// ApplyMutations applies the mutations to the state atomically.
func (st *Store) ApplyMutations(mu *state.Mutations) error {
	st.Lock()
	defer st.Unlock()
	if err := st.State.ApplyMutations(mu); err != nil {
		return err
	}
	if st.log != nil {
		// Append the transaction to the log.
		if err := st.log.appendTransaction(mu); err != nil {
			// We can't continue because the log failed.  The state has already been updated,
			// but access to the state is blocked because we have the lock.  Close the state
			// to ensure that it is never used again.
			st.closeLocked()
			return err
		}
	}
	return nil
}

// PutMutations atomically commits a stream of Mutations when the stream is
// closed. Mutations are not committed if the request is cancelled before the
// stream has been closed.
func (st *Store) PutMutations(ctx ipc.ServerContext, stream raw.StoreServicePutMutationsStream) error {
	tr := st.newNilTransaction()
	rStream := stream.RecvStream()
	for rStream.Advance() {
		mu := rStream.Value()

		if err := tr.snapshot.PutMutation(mu); err != nil {
			tr.Abort()
			return err
		}
	}
	err := rStream.Err()
	if err != nil {
		tr.Abort()
		return err
	}

	if ctx.IsClosed() {
		tr.Abort()
		return ErrRequestCancelled
	}
	return tr.Commit()
}

// SetConflictResolver specifies a function to perform conflict resolution.
// The <ty> represents the IDL name for the type.
func (st *Store) SetConflictResolver(ty string, r storage.ConflictResolver) {
	panic("not implemented")
}
