// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package naming

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	inamespace "v.io/x/ref/profiles/internal/naming/namespace"
)

// NewSimpleNamespace returns a simple implementation of a Namespace
// server for use in tests.  In particular, it ignores TTLs and not
// allow fully overlapping mount names.
func NewSimpleNamespace() namespace.T {
	ns, err := inamespace.New()
	if err != nil {
		panic(err)
	}
	return &namespaceMock{mounts: make(map[string]*naming.MountEntry), ns: ns}
}

// namespaceMock is a simple partial implementation of namespace.T.
type namespaceMock struct {
	sync.Mutex
	mounts map[string]*naming.MountEntry
	ns     namespace.T
}

func (ns *namespaceMock) Mount(ctx *context.T, name, server string, _ time.Duration, opts ...naming.NamespaceOpt) error {
	defer vlog.LogCall()()
	ns.Lock()
	defer ns.Unlock()
	for n, _ := range ns.mounts {
		if n != name && (strings.HasPrefix(name, n) || strings.HasPrefix(n, name)) {
			return fmt.Errorf("simple mount table does not allow names that are a prefix of each other")
		}
	}
	e := ns.mounts[name]
	if e == nil {
		e = &naming.MountEntry{}
		ns.mounts[name] = e
	}
	e.Servers = append(e.Servers, naming.MountedServer{Server: server})
	return nil
}

func (ns *namespaceMock) Unmount(ctx *context.T, name, server string, opts ...naming.NamespaceOpt) error {
	defer vlog.LogCall()()
	ns.Lock()
	defer ns.Unlock()
	e := ns.mounts[name]
	if e == nil {
		return nil
	}
	if len(server) == 0 {
		delete(ns.mounts, name)
		return nil
	}
	var keep []naming.MountedServer
	for _, s := range e.Servers {
		if s.Server != server {
			keep = append(keep, s)
		}
	}
	if len(keep) == 0 {
		delete(ns.mounts, name)
		return nil
	}
	e.Servers = keep
	return nil
}

func (ns *namespaceMock) Delete(ctx *context.T, name string, removeSubtree bool, opts ...naming.NamespaceOpt) error {
	defer vlog.LogCall()()
	ns.Lock()
	defer ns.Unlock()
	e := ns.mounts[name]
	if e == nil {
		return nil
	}
	delete(ns.mounts, name)
	if !removeSubtree {
		return nil
	}
	for k := range ns.mounts {
		if strings.HasPrefix(k, name+"/") {
			delete(ns.mounts, k)
		}
	}
	return nil
}

func (ns *namespaceMock) Resolve(ctx *context.T, name string, opts ...naming.NamespaceOpt) (*naming.MountEntry, error) {
	defer vlog.LogCall()()
	_, name = security.SplitPatternName(name)
	if address, suffix := naming.SplitAddressName(name); len(address) > 0 {
		return &naming.MountEntry{
			Name:    suffix,
			Servers: []naming.MountedServer{{Server: address}},
		}, nil
	}
	ns.Lock()
	defer ns.Unlock()
	for prefix, e := range ns.mounts {
		if strings.HasPrefix(name, prefix) {
			ret := *e
			ret.Name = strings.TrimLeft(strings.TrimPrefix(name, prefix), "/")
			return &ret, nil
		}
	}
	return nil, verror.New(naming.ErrNoSuchName, ctx, fmt.Sprintf("Resolve name %q not found in %v", name, ns.mounts))
}

func (ns *namespaceMock) ResolveToMountTable(ctx *context.T, name string, opts ...naming.NamespaceOpt) (*naming.MountEntry, error) {
	defer vlog.LogCall()()
	// TODO(mattr): Implement this method for tests that might need it.
	panic("ResolveToMountTable not implemented")
	return nil, nil
}

func (ns *namespaceMock) FlushCacheEntry(name string) bool {
	defer vlog.LogCall()()
	return false
}

func (ns *namespaceMock) CacheCtl(ctls ...naming.CacheCtl) []naming.CacheCtl {
	defer vlog.LogCall()()
	return nil
}

func (ns *namespaceMock) Glob(ctx *context.T, pattern string, opts ...naming.NamespaceOpt) (chan naming.GlobReply, error) {
	defer vlog.LogCall()()
	// TODO(mattr): Implement this method for tests that might need it.
	panic("Glob not implemented")
	return nil, nil
}

func (ns *namespaceMock) SetRoots(...string) error {
	defer vlog.LogCall()()
	panic("Calling SetRoots on a mock namespace.  This is not supported.")
	return nil
}

func (ns *namespaceMock) Roots() []string {
	defer vlog.LogCall()()
	panic("Calling Roots on a mock namespace.  This is not supported.")
	return nil
}

func (ns *namespaceMock) GetPermissions(ctx *context.T, name string, opts ...naming.NamespaceOpt) (perms access.Permissions, version string, err error) {
	defer vlog.LogCall()()
	panic("Calling GetPermissions on a mock namespace.  This is not supported.")
	return nil, "", nil
}

func (ns *namespaceMock) SetPermissions(ctx *context.T, name string, perms access.Permissions, version string, opts ...naming.NamespaceOpt) error {
	defer vlog.LogCall()()
	panic("Calling SetPermissions on a mock namespace.  This is not supported.")
	return nil
}
