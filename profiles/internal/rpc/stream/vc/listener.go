// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"v.io/v23/verror"

	"v.io/x/ref/profiles/internal/lib/upcqueue"
	"v.io/x/ref/profiles/internal/rpc/stream"
)

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errListenerClosed = reg(".errListenerClosed", "Listener has been closed")
	errGetFromQueue   = reg(".errGetFromQueue", "upcqueue.Get failed{:3}")
)

type listener struct {
	q *upcqueue.T
}

var _ stream.Listener = (*listener)(nil)

func newListener() *listener { return &listener{q: upcqueue.New()} }

func (l *listener) Enqueue(f stream.Flow) error {
	err := l.q.Put(f)
	if err == upcqueue.ErrQueueIsClosed {
		return verror.New(stream.ErrBadState, nil, verror.New(errListenerClosed, nil))
	}
	return err
}

func (l *listener) Accept() (stream.Flow, error) {
	item, err := l.q.Get(nil)
	if err == upcqueue.ErrQueueIsClosed {
		return nil, verror.New(stream.ErrBadState, nil, verror.New(errListenerClosed, nil))
	}
	if err != nil {
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errGetFromQueue, nil, err))
	}
	return item.(stream.Flow), nil
}

func (l *listener) Close() error {
	l.q.Close()
	return nil
}
