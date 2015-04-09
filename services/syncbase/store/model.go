// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package store defines the API for the syncbase storage engine.
// Currently, this API and its implementations are meant to be internal.
package store

// Store is a CRUD-capable storage engine.
// TODO(sadovsky): For convenience, we use string (rather than []byte) as the
// key type. Maybe revisit this.
// TODO(sadovsky): Maybe separate Insert/Update/Upsert for implementation
// convenience. Or, provide these as helpers on top of the API below.
type Store interface {
	// Get returns the value for the given key.
	// Fails if the given key is unknown (ErrUnknownKey).
	Get(k string) ([]byte, error)

	// Put writes the given value for the given key.
	Put(k string, v []byte) error

	// Delete deletes the entry for the given key.
	// Succeeds (no-op) if the given key is unknown.
	Delete(k string) error
}

// TransactableStore is a Store that supports transactions.
// It should be possible to implement using LevelDB, SQLite, or similar.
type TransactableStore interface {
	Store

	// CreateTransaction creates a transaction.
	CreateTransaction() Transaction

	// TODO(sadovsky): Figure out how sync's "PutMutations" fits in here. In
	// theory it avoids a read (since it knows the version, i.e. the etag, up
	// front), but OTOH that version still must be read at commit time (for
	// verification), so reading it in a transaction (and thus adding it to the
	// transaction's read-set) incurs no extra cost if we use transaction
	// implementation strategy #1 from the doc. That said, strategy #1 is not
	// tenable in a cloud environment. Perhaps Transaction should provide some
	// mechanism for directly adding to its read-set (without actually reading)?
}

// Transaction provides a mechanism for atomic reads and writes.
// Reads don't reflect writes performed inside this transaction. (This
// limitation is imposed for API parity with Spanner.)
// Once a transaction has been committed or aborted, subsequent method calls
// will fail with no effect.
type Transaction interface {
	Store

	// Commit commits the transaction.
	Commit() error

	// Abort aborts the transaction.
	Abort() error
}

// TODO(sadovsky): Maybe put this elsewhere.
// TODO(sadovsky): Add retry loop.
func RunInTransaction(st TransactableStore, fn func(st Store) error) error {
	tx := st.CreateTransaction()
	if err := fn(tx); err != nil {
		tx.Abort()
		return err
	}
	if err := tx.Commit(); err != nil {
		tx.Abort()
		return err
	}
	return nil
}

type ErrUnknownKey struct {
	Key string
}

func (err *ErrUnknownKey) Error() string {
	return "unknown key: " + err.Key
}
