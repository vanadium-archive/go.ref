// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package store defines the API for the syncbase storage engine.
// Currently, this API and its implementations are meant to be internal.
package store

// StoreReader reads data from a CRUD-capable storage engine.
type StoreReader interface {
	// Scan returns all rows with keys in range [start, end).
	Scan(start, end string) (Stream, error)

	// Get returns the value for the given key.
	// Fails if the given key is unknown (ErrUnknownKey).
	Get(key string) ([]byte, error)
}

// StoreWriter writes data to a CRUD-capable storage engine.
type StoreWriter interface {
	// Put writes the given value for the given key.
	Put(key string, v []byte) error

	// Delete deletes the entry for the given key.
	// Succeeds (no-op) if the given key is unknown.
	Delete(key string) error
}

// Store is a CRUD-capable storage engine that supports transactions.
type Store interface {
	StoreReader
	StoreWriter

	// NewTransaction creates a transaction.
	// TODO(rogulenko): add transaction options.
	NewTransaction() Transaction

	// NewSnapshot creates a snapshot.
	// TODO(rogulenko): add snapshot options.
	NewSnapshot() Snapshot
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
	StoreReader
	StoreWriter

	// Commit commits the transaction.
	Commit() error

	// Abort aborts the transaction.
	Abort() error

	// ResetForRetry resets the transaction. It's equivalent to aborting the
	// transaction and creating a new one, but more efficient.
	ResetForRetry()
}

// Snapshot is a handle to particular state in time of a Store.
// All read operations are executed at a consistent snapshot of Store commit
// history. Snapshots don't acquire locks and thus don't block transactions.
type Snapshot interface {
	StoreReader

	// Close closes a previously acquired snapshot.
	// Any subsequent method calls will return errors.
	Close() error
}

// KeyValue is a wrapper for the key and value from a single row.
type KeyValue struct {
	Key   string
	Value []byte
}

// Stream is an interface for iterating through a collection of key-value pairs.
type Stream interface {
	// Advance stages an element so the client can retrieve it with Value. Advance
	// returns true iff there is an element to retrieve. The client must call
	// Advance before calling Value. The client must call Cancel if it does not
	// iterate through all elements (i.e. until Advance returns false). Advance
	// may block if an element is not immediately available.
	Advance() bool

	// Value returns the element that was staged by Advance. Value may panic if
	// Advance returned false or was not called at all. Value does not block.
	Value() KeyValue

	// Err returns a non-nil error iff the stream encountered any errors. Err does
	// not block.
	Err() error

	// Cancel notifies the stream provider that it can stop producing elements.
	// The client must call Cancel if it does not iterate through all elements
	// (i.e. until Advance returns false). Cancel is idempotent and can be called
	// concurrently with a goroutine that is iterating via Advance/Value. Cancel
	// causes Advance to subsequently return false. Cancel does not block.
	Cancel()
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
