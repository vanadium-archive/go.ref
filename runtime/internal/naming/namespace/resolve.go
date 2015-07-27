// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package namespace

import (
	"errors"
	"runtime"
	"strings"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/ref/lib/apilog"
)

func (ns *namespace) resolveAgainstMountTable(ctx *context.T, client rpc.Client, e *naming.MountEntry, opts ...rpc.CallOpt) (*naming.MountEntry, error) {
	// Try each server till one answers.
	finalErr := errors.New("no servers to resolve query")
	opts = append(opts, options.NoResolve{})
	for _, s := range e.Servers {
		// If the server was not specified as an endpoint (perhaps as host:port)
		// then we really don't know if this is a mounttable or not.  Check the
		// cache to see if we've tried in the recent past and it came back as not
		// a mounttable.
		if ns.resolutionCache.isNotMT(s.Server) {
			finalErr = verror.New(verror.ErrUnknownMethod, nil, "ResolveStep")
			continue
		}
		// Assume a mount table and make the call.
		name := naming.JoinAddressName(s.Server, e.Name)
		// First check the cache.
		if ne, err := ns.resolutionCache.lookup(ctx, name); err == nil {
			ctx.VI(2).Infof("resolveAMT %s from cache -> %v", name, convertServersToStrings(ne.Servers, ne.Name))
			return &ne, nil
		}
		// Not in cache, call the real server.
		callCtx := ctx
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			// Only set a per-call timeout if a deadline has not already
			// been set.
			callCtx = withTimeout(ctx)
		}
		entry := new(naming.MountEntry)
		if err := client.Call(callCtx, name, "ResolveStep", nil, []interface{}{entry}, opts...); err != nil {
			// If any replica says the name doesn't exist, return that fact.
			if verror.ErrorID(err) == naming.ErrNoSuchName.ID || verror.ErrorID(err) == naming.ErrNoSuchNameRoot.ID {
				return nil, err
			}
			// If it wasn't a mounttable remember that fact.  The check for the __ is for
			// the debugging hack in the local namespace of every server.  That part never
			// answers mounttable RPCs and shouldn't make us think this isn't a mounttable
			// server.
			if notAnMT(err) && !strings.HasPrefix(e.Name, "__") {
				ns.resolutionCache.setNotMT(s.Server)
			}
			// Keep track of the final error and continue with next server.
			finalErr = err
			ctx.VI(2).Infof("resolveAMT: Finish %s failed: %s", name, err)
			continue
		}
		// Add result to cache.
		ns.resolutionCache.remember(ctx, name, entry)
		ctx.VI(2).Infof("resolveAMT %s -> %v", name, entry)
		return entry, nil
	}
	ctx.VI(2).Infof("resolveAMT %v -> %v", e.Servers, finalErr)
	return nil, finalErr
}

func terminal(e *naming.MountEntry) bool {
	return len(e.Name) == 0
}

// Resolve implements v.io/v23/naming.Namespace.
func (ns *namespace) Resolve(ctx *context.T, name string, opts ...naming.NamespaceOpt) (*naming.MountEntry, error) {
	defer apilog.LogCallf(ctx, "name=%.10s...,opts...=%v", name, opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	e, _ := ns.rootMountEntry(name, opts...)
	if ctx.V(2) {
		_, file, line, _ := runtime.Caller(1)
		ctx.Infof("Resolve(%s) called from %s:%d", name, file, line)
		ctx.Infof("Resolve(%s) -> rootMountEntry %v", name, *e)
	}
	if skipResolve(opts) {
		return e, nil
	}
	if len(e.Servers) == 0 {
		return nil, verror.New(naming.ErrNoSuchName, ctx, name)
	}
	client := v23.GetClient(ctx)
	callOpts := getCallOpts(opts)

	// Iterate walking through mount table servers.
	for remaining := ns.maxResolveDepth; remaining > 0; remaining-- {
		ctx.VI(2).Infof("Resolve(%s) loop %v", name, *e)
		if !e.ServesMountTable || terminal(e) {
			ctx.VI(1).Infof("Resolve(%s) -> %v", name, *e)
			return e, nil
		}
		var err error
		curr := e
		if e, err = ns.resolveAgainstMountTable(ctx, client, curr, callOpts...); err != nil {
			// Lots of reasons why another error can happen.  We are trying
			// to single out "this isn't a mount table".
			if notAnMT(err) {
				ctx.VI(1).Infof("Resolve(%s) -> %v", name, curr)
				return curr, nil
			}
			if verror.ErrorID(err) == naming.ErrNoSuchNameRoot.ID {
				err = verror.New(naming.ErrNoSuchName, ctx, name)
			}
			ctx.VI(1).Infof("Resolve(%s) -> (%s: %v)", err, name, curr)
			return nil, err
		}
	}
	return nil, verror.New(naming.ErrResolutionDepthExceeded, ctx)
}

// ResolveToMountTable implements v.io/v23/naming.Namespace.
func (ns *namespace) ResolveToMountTable(ctx *context.T, name string, opts ...naming.NamespaceOpt) (*naming.MountEntry, error) {
	defer apilog.LogCallf(ctx, "name=%.10s...,opts...=%v", name, opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	e, _ := ns.rootMountEntry(name, opts...)
	if ctx.V(2) {
		_, file, line, _ := runtime.Caller(1)
		ctx.Infof("ResolveToMountTable(%s) called from %s:%d", name, file, line)
		ctx.Infof("ResolveToMountTable(%s) -> rootNames %v", name, e)
	}
	if len(e.Servers) == 0 {
		return nil, verror.New(naming.ErrNoMountTable, ctx)
	}
	callOpts := getCallOpts(opts)
	client := v23.GetClient(ctx)
	last := e
	for remaining := ns.maxResolveDepth; remaining > 0; remaining-- {
		ctx.VI(2).Infof("ResolveToMountTable(%s) loop %v", name, e)
		var err error
		curr := e
		// If the next name to resolve doesn't point to a mount table, we're done.
		if !e.ServesMountTable || terminal(e) {
			ctx.VI(1).Infof("ResolveToMountTable(%s) -> %v", name, last)
			return last, nil
		}
		if e, err = ns.resolveAgainstMountTable(ctx, client, e, callOpts...); err != nil {
			if verror.ErrorID(err) == naming.ErrNoSuchNameRoot.ID {
				ctx.VI(1).Infof("ResolveToMountTable(%s) -> %v (NoSuchRoot: %v)", name, last, curr)
				return last, nil
			}
			if verror.ErrorID(err) == naming.ErrNoSuchName.ID {
				ctx.VI(1).Infof("ResolveToMountTable(%s) -> %v (NoSuchName: %v)", name, curr, curr)
				return curr, nil
			}
			// Lots of reasons why another error can happen.  We are trying
			// to single out "this isn't a mount table".
			if notAnMT(err) {
				ctx.VI(1).Infof("ResolveToMountTable(%s) -> %v", name, last)
				return last, nil
			}
			// TODO(caprita): If the server is unreachable for
			// example, we may still want to return its parent
			// mounttable rather than an error.
			ctx.VI(1).Infof("ResolveToMountTable(%s) -> %v", name, err)
			return nil, err
		}
		last = curr
	}
	return nil, verror.New(naming.ErrResolutionDepthExceeded, ctx)
}

// FlushCache flushes the most specific entry found for name.  It returns true if anything was
// actually flushed.
func (ns *namespace) FlushCacheEntry(ctx *context.T, name string) bool {
	defer apilog.LogCallf(nil, "name=%.10s...", name)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	flushed := false
	for _, n := range ns.rootName(name) {
		// Walk the cache as we would in a resolution.  Unlike a resolution, we have to follow
		// all branches since we want to flush all entries at which we might end up whereas in a resolution,
		// we stop with the first branch that works.
		if e, err := ns.resolutionCache.lookup(ctx, n); err == nil {
			// Recurse.
			for _, s := range e.Servers {
				flushed = flushed || ns.FlushCacheEntry(ctx, naming.Join(s.Server, e.Name))
			}
			if !flushed {
				// Forget the entry we just used.
				ns.resolutionCache.forget(ctx, []string{naming.TrimSuffix(n, e.Name)})
				flushed = true
			}
		}
	}
	return flushed
}

func skipResolve(opts []naming.NamespaceOpt) bool {
	for _, o := range opts {
		if _, ok := o.(options.NoResolve); ok {
			return true
		}
	}
	return false
}
