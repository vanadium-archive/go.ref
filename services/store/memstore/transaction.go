package memstore

import (
	"sync"

	"veyron/services/store/memstore/state"
)

// Transaction is the type of transactions.  Each transaction has a snapshot of
// the entire state of the store; <store> is the shared *Store, assigned when
// the transaction was first used, and <snapshot> is MutableSnapshot that
// includes any changes in the transaction.
//
// Transactions are initially empty.  The <store> and <snapshot> fields are
// set when the transaction is first used.
type Transaction struct {
	mutex    sync.Mutex
	store    *Store
	snapshot *state.MutableSnapshot
}

// newNilTransaction is used when nil is passed in as the transaction for an
// object operation.  This means that the operation is to be performed on the
// state <st>.
func (st *Store) newNilTransaction() *Transaction {
	st.Lock()
	defer st.Unlock()
	return &Transaction{store: st, snapshot: st.State.MutableSnapshot()}
}

// getTransaction returns the *Transaction value for the service.Transaction.
// Returns bool commit==true iff the transaction argument is nil, which means
// that the transaction lifetime is the duration of the operation (so the
// transaction should be committed immediately after the operation that uses it
// is performed).
func (st *Store) getTransaction(tr *Transaction) (*Transaction, bool, error) {
	if tr == nil {
		return st.newNilTransaction(), true, nil
	}
	tr.useState(st)
	return tr, false, nil
}

// GetTransactionSnapshot returns a read-only snapshot from the transaction.
func (st *Store) GetTransactionSnapshot(tr *Transaction) (state.Snapshot, error) {
	t, _, err := st.getTransaction(tr)
	if err != nil {
		return nil, err
	}
	return t.Snapshot(), nil
}

// NewTransaction returns a fresh transaction containing no changes.
func NewTransaction() *Transaction {
	return &Transaction{}
}

// useState sets the state in the transaction if it hasn't been set already.
func (t *Transaction) useState(st *Store) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.store == nil {
		t.store = st
		t.snapshot = st.State.MutableSnapshot()
	}
}

// Commit commits the transaction.  The commit may abort if there have been
// concurrent changes to the store that conflict with the changes in the
// transaction.  If so, Commit returns an error and leaves the store unchanged.
// The Commit is atomic -- all of the changes in the transaction are applied to
// the state, or none of them are.
func (t *Transaction) Commit() error {
	t.mutex.Lock()
	st, sn := t.store, t.snapshot
	t.mutex.Unlock()
	if st == nil || sn == nil {
		return nil
	}
	err := st.ApplyMutations(sn.Mutations())
	// Log deleted objects via garbage collection. This occurs within the
	// transaction boundary, i.e. before the state lock is released.
	// TODO(tilaks): separate discovery and collection, collect lazily.
	st.maybeGC(err)
	return err
}

// maybeGC will run a garbage collection if the committed transaction may result
// in an orphaned object.
// For now, the heuristic is simple - if the transaction succeeded, run GC().
func (st *Store) maybeGC(err error) {
	if err == nil {
		st.GC()
	}
}

// Abort discards a transaction.  This is an optimization; transactions
// eventually time out and get discarded.  However, live transactions
// consume resources, so it is good practice to clean up.
func (t *Transaction) Abort() error {
	t.mutex.Lock()
	t.store = nil
	t.snapshot = nil
	t.mutex.Unlock()
	return nil
}

// Snapshot returns a read-only snapshot.
func (t *Transaction) Snapshot() state.Snapshot {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.snapshot.GetSnapshot()
}
