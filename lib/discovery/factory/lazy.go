// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"errors"
	"sync"

	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"
)

var (
	errClosed = errors.New("factory closed")
)

// lazyFactory is a simple wrapper that creates a new discovery instance lazily
// when Advertise() or Scan() is called for the first time.
type lazyFactory struct {
	newInstance func() (discovery.T, error)

	once sync.Once
	d    discovery.T
	derr error
}

func (l *lazyFactory) Advertise(ctx *context.T, service *discovery.Service, visibility []security.BlessingPattern) (<-chan struct{}, error) {
	l.once.Do(l.init)
	if l.derr != nil {
		return nil, l.derr
	}
	return l.d.Advertise(ctx, service, visibility)
}

func (l *lazyFactory) Scan(ctx *context.T, query string) (<-chan discovery.Update, error) {
	l.once.Do(l.init)
	if l.derr != nil {
		return nil, l.derr
	}
	return l.d.Scan(ctx, query)
}

func (l *lazyFactory) Close() {
	l.once.Do(l.noinit)
	if l.d != nil {
		l.d.Close()
	}
}

func (l *lazyFactory) init() {
	l.d, l.derr = l.newInstance()
}

func (l *lazyFactory) noinit() {
	l.derr = errClosed
}

func newLazyFactory(newInstance func() (discovery.T, error)) discovery.T {
	return &lazyFactory{newInstance: newInstance}
}
