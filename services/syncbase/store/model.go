// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package store defines the API for the syncbase storage engine.
// Currently, this API and its implementations are meant to be internal.
package store

// StoreReader reads data from a CRUD-capable storage engine.
type StoreReader interface {
	// Scan returns all rows with keys in range [start, end).
	Scan(start, end []byte) (Stream, error)

	// Get returns the value for the given key. The returned slice may be a
	// sub-slice of valbuf if valbuf was large enough to hold the entire value.
	// Otherwise, a newly allocated slice will be returned. It is valid to pass
	// a nil valbuf.
	// Fails if the given key is unknown (ErrUnknownKey).
	Get(key, valbuf []byte) ([]byte, error)
}

// StoreWriter writes data to a CRUD-capable storage engine.
type StoreWriter interface {
	// Put writes the given value for the given key.
	Put(key, value []byte) error

	// Delete deletes the entry for the given key.
	// Succeeds (no-op) if the given key is unknown.
	Delete(key []byte) error
}

// StoreReadWriter combines StoreReader and StoreWriter.
type StoreReadWriter interface {
	StoreReader
	StoreWriter
}

// Store is a CRUD-capable storage engine that supports transactions.
type Store interface {
	StoreReadWriter

	// NewTransaction creates a transaction.
	// TODO(rogulenko): add transaction options.
	NewTransaction() Transaction

	// NewSnapshot creates a snapshot.
	// TODO(rogulenko): add snapshot options.
	NewSnapshot() Snapshot

	// Close closes the store.
	Close() error
}

// Transaction provides a mechanism for atomic reads and writes.
// Reads don't reflect writes performed inside this transaction. (This
// limitation is imposed for API parity with Spanner.)
// Once a transaction has been committed or aborted, subsequent method calls
// will fail with no effect.
//
// Operations on a transaction may start failing with ErrConcurrentTransaction
// if writes from a newly-committed transaction conflict with reads or writes
// from this transaction.
type Transaction interface {
	StoreReadWriter

	// Commit commits the transaction.
	Commit() error

	// Abort aborts the transaction.
	Abort() error

	// ResetForRetry resets the transaction. It's equivalent to aborting the
	// transaction and creating a new one, but more efficient.
	ResetForRetry()
}

// Snapshot is a handle to particular state in time of a Store.
// All read operations are executed against a consistent snapshot of Store
// commit history. Snapshots don't acquire locks and thus don't block
// transactions.
type Snapshot interface {
	StoreReader

	// Close closes the snapshot.
	// Any subsequent method calls will fail.
	Close() error
}

// Stream is an interface for iterating through a collection of key-value pairs.
type Stream interface {
	// Advance stages an element so the client can retrieve it with Key or Value.
	// Advance returns true iff there is an element to retrieve. The client must
	// call Advance before calling Key or Value. The client must call Cancel if it
	// does not iterate through all elements (i.e. until Advance returns false).
	// Advance may block if an element is not immediately available.
	Advance() bool

	// Key returns the key of the element that was staged by Advance. The returned
	// slice may be a sub-slice of keybuf if keybuf was large enough to hold the
	// entire key. Otherwise, a newly allocated slice will be returned. It is
	// valid to pass a nil keybuf.
	// Key may panic if Advance returned false or was not called at all.
	// Key does not block.
	Key(keybuf []byte) []byte

	// Value returns the value of the element that was staged by Advance. The
	// returned slice may be a sub-slice of valbuf if valbuf was large enough to
	// hold the entire value. Otherwise, a newly allocated slice will be returned.
	// It is valid to pass a nil valbuf.
	// Value may panic if Advance returned false or was not called at all.
	// Value does not block.
	Value(valbuf []byte) []byte

	// Err returns a non-nil error iff the stream encountered any errors. Err does
	// not block.
	// TODO(rogulenko): define an error type.
	Err() error

	// Cancel notifies the stream provider that it can stop producing elements.
	// The client must call Cancel if it does not iterate through all elements
	// (i.e. until Advance returns false). Cancel is idempotent and can be called
	// concurrently with a goroutine that is iterating via Advance/Key/Value.
	// Cancel causes Advance to subsequently return false. Cancel does not block.
	Cancel()
}

// TODO(sadovsky): Add retry loop.
func RunInTransaction(st Store, fn func(st StoreReadWriter) error) error {
	tx := st.NewTransaction()
	if err := fn(tx); err != nil {
		tx.Abort()
		return err
	}
	if err := tx.Commit(); err != nil {
		// TODO(sadovsky): Commit() can fail for a number of reasons, e.g. RPC
		// failure or ErrConcurrentTransaction. Depending on the cause of failure,
		// it may be desirable to retry the Commit() and/or to call Abort(). For
		// now, we always abort on a failed commit.
		tx.Abort()
		return err
	}
	return nil
}

type ErrConcurrentTransaction struct{}

func (e *ErrConcurrentTransaction) Error() string {
	return "concurrent transaction"
}

type ErrUnknownKey struct {
	Key string
}

func (err *ErrUnknownKey) Error() string {
	return "unknown key: " + err.Key
}
