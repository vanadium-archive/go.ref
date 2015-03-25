// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"errors"

	"v.io/x/ref/profiles/internal/lib/upcqueue"
	"v.io/x/ref/profiles/internal/rpc/stream"
)

var errListenerClosed = errors.New("Listener has been closed")

type listener struct {
	q *upcqueue.T
}

var _ stream.Listener = (*listener)(nil)

func newListener() *listener { return &listener{q: upcqueue.New()} }

func (l *listener) Enqueue(f stream.Flow) error {
	err := l.q.Put(f)
	if err == upcqueue.ErrQueueIsClosed {
		return errListenerClosed
	}
	return err
}

func (l *listener) Accept() (stream.Flow, error) {
	item, err := l.q.Get(nil)
	if err == upcqueue.ErrQueueIsClosed {
		return nil, errListenerClosed
	}
	if err != nil {
		return nil, err
	}
	return item.(stream.Flow), nil
}

func (l *listener) Close() error {
	l.q.Close()
	return nil
}
