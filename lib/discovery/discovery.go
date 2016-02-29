// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"sync"

	"v.io/v23/context"
	"v.io/v23/discovery"
)

type sessionId uint64

type idiscovery struct {
	plugins []Plugin

	mu     sync.Mutex
	closed bool                  // GUARDED_BY(mu)
	tasks  map[*context.T]func() // GUARDED_BY(mu)
	wg     sync.WaitGroup

	ads map[discovery.AdId]sessionId // GUARDED_BY(mu)
}

func (d *idiscovery) shutdown() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	for _, cancel := range d.tasks {
		cancel()
	}
	d.closed = true
	d.mu.Unlock()
	d.wg.Wait()
}

func (d *idiscovery) addTask(ctx *context.T) (*context.T, func(), error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, nil, NewErrDiscoveryClosed(ctx)
	}
	ctx, cancel := context.WithCancel(ctx)
	d.tasks[ctx] = cancel
	d.wg.Add(1)
	d.mu.Unlock()
	return ctx, cancel, nil
}

func (d *idiscovery) removeTask(ctx *context.T) {
	d.mu.Lock()
	if _, exist := d.tasks[ctx]; exist {
		delete(d.tasks, ctx)
		d.wg.Done()
	}
	d.mu.Unlock()
}

func newDiscovery(ctx *context.T, plugins []Plugin) (*idiscovery, error) {
	if len(plugins) == 0 {
		return nil, NewErrNoDiscoveryPlugin(ctx)
	}
	d := &idiscovery{
		plugins: make([]Plugin, len(plugins)),
		tasks:   make(map[*context.T]func()),
		ads:     make(map[discovery.AdId]sessionId),
	}
	copy(d.plugins, plugins)
	return d, nil
}
