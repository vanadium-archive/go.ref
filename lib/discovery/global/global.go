// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO(jhahn): This is an experimental work to see its feasibility and set
// the long-term goal, and can be changed without notice.
//
// TODO(jhahn): There are many duplicate codes between "v.io/x/ref/lib/discovery"
// and this package. Refactor them.
package global

import (
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/verror"

	"v.io/x/ref/lib/timekeeper"
)

const pkgPath = "v.io/x/ref/lib/discovery/global"

var (
	errNoNamespace = verror.Register(pkgPath+".errNoNamespace", verror.NoRetry, "{1:}{2:} namespace not found")
)

type gdiscovery struct {
	ns namespace.T

	mu  sync.Mutex
	ads map[string]struct{} // GUARDED_BY(mu)

	clock timekeeper.TimeKeeper
}

// New returns a new global Discovery.T instance that uses the Vanadium
// namespace under 'path'.
func New(ctx *context.T, path string) (discovery.T, error) {
	return newWithClock(ctx, path, timekeeper.RealTime())
}

func newWithClock(ctx *context.T, path string, clock timekeeper.TimeKeeper) (discovery.T, error) {
	ns := v23.GetNamespace(ctx)
	if ns == nil {
		return nil, verror.New(errNoNamespace, ctx)
	}

	var roots []string
	if naming.Rooted(path) {
		roots = []string{path}
	} else {
		for _, root := range ns.Roots() {
			roots = append(roots, naming.Join(root, path))
		}
	}
	_, ns, err := v23.WithNewNamespace(ctx, roots...)
	if err != nil {
		return nil, err
	}
	ns.CacheCtl(naming.DisableCache(true))

	d := &gdiscovery{
		ns:    ns,
		ads:   make(map[string]struct{}),
		clock: clock,
	}
	return d, nil
}
