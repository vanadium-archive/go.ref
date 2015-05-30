// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package store

import (
	"v.io/v23/verror"
)

func RunInTransaction(st Store, fn func(st StoreReadWriter) error) error {
	// TODO(rogulenko): We should eventually give up with
	// ErrConcurrentTransaction.
	// TODO(rogulenko): Fail on RPC errors.
	for {
		tx := st.NewTransaction()
		if err := fn(tx); err != nil {
			tx.Abort()
			return err
		}
		err := tx.Commit()
		if err == nil {
			return nil
		}
		if verror.ErrorID(err) == ErrConcurrentTransaction.ID {
			continue
		}
		return err
	}
}

// CopyBytes copies elements from a source slice into a destination slice.
// The returned slice may be a sub-slice of dst if dst was large enough to hold
// src. Otherwise, a newly allocated slice will be returned.
// TODO(rogulenko): add some tests.
func CopyBytes(dst, src []byte) []byte {
	if cap(dst) < len(src) {
		newlen := cap(dst)*2 + 2
		if newlen < len(src) {
			newlen = len(src)
		}
		dst = make([]byte, newlen)
	}
	dst = dst[:len(src)]
	copy(dst, src)
	return dst
}

// WrapError wraps the given error with a verror. It is a no-op if the given
// error is nil. The returned error contains the current stack trace, but
// retains the input error's IDAction pair. If the given error is not a verror,
// the IDAction pair of the returned error will be (ErrUnknown.ID, NoRetry).
func WrapError(err error) error {
	if err == nil {
		return nil
	}
	return verror.New(verror.IDAction{
		verror.ErrorID(err),
		verror.Action(err),
	}, nil, err)
}
