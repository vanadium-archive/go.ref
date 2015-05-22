// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package store

// InvalidSnapshot is a store.Snapshot for which all methods return errors.
type InvalidSnapshot struct {
	// Error is the error returned by every method call.
	Error error
}

// Close implements the store.Snapshot interface.
func (s *InvalidSnapshot) Close() error {
	return WrapError(s.Error)
}

// Get implements the store.StoreReader interface.
func (s *InvalidSnapshot) Get(key, valbuf []byte) ([]byte, error) {
	return valbuf, WrapError(s.Error)
}

// Scan implements the store.StoreReader interface.
func (s *InvalidSnapshot) Scan(start, limit []byte) Stream {
	return &InvalidStream{s.Error}
}

// InvalidStream is a store.Stream for which all methods return errors.
type InvalidStream struct {
	// Error is the error returned by every method call.
	Error error
}

// Advance implements the store.Stream interface.
func (s *InvalidStream) Advance() bool {
	return false
}

// Key implements the store.Stream interface.
func (s *InvalidStream) Key(keybuf []byte) []byte {
	panic(s.Error)
}

// Value implements the store.Stream interface.
func (s *InvalidStream) Value(valbuf []byte) []byte {
	panic(s.Error)
}

// Err implements the store.Stream interface.
func (s *InvalidStream) Err() error {
	return WrapError(s.Error)
}

// Cancel implements the store.Stream interface.
func (s *InvalidStream) Cancel() {
}

// InvalidTransaction is a store.Transaction for which all methods return errors.
type InvalidTransaction struct {
	// Error is the error returned by every method call.
	Error error
}

// ResetForRetry implements the store.Transaction interface.
func (tx *InvalidTransaction) ResetForRetry() {
	panic(tx.Error)
}

// Get implements the store.StoreReader interface.
func (tx *InvalidTransaction) Get(key, valbuf []byte) ([]byte, error) {
	return valbuf, WrapError(tx.Error)
}

// Scan implements the store.StoreReader interface.
func (tx *InvalidTransaction) Scan(start, limit []byte) Stream {
	return &InvalidStream{tx.Error}
}

// Put implements the store.StoreWriter interface.
func (tx *InvalidTransaction) Put(key, value []byte) error {
	return WrapError(tx.Error)
}

// Delete implements the store.StoreWriter interface.
func (tx *InvalidTransaction) Delete(key []byte) error {
	return WrapError(tx.Error)
}

// Commit implements the store.Transaction interface.
func (tx *InvalidTransaction) Commit() error {
	return WrapError(tx.Error)
}

// Abort implements the store.Transaction interface.
func (tx *InvalidTransaction) Abort() error {
	return WrapError(tx.Error)
}
