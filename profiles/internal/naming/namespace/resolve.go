package namespace

import (
	"errors"
	"runtime"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

func (ns *namespace) resolveAgainstMountTable(ctx *context.T, client rpc.Client, e *naming.MountEntry, opts ...rpc.CallOpt) (*naming.MountEntry, error) {
	// Try each server till one answers.
	finalErr := errors.New("no servers to resolve query")
	opts = append(opts, options.NoResolve{})
	for _, s := range e.Servers {
		name := naming.JoinAddressName(s.Server, e.Name)
		// First check the cache.
		if ne, err := ns.resolutionCache.lookup(name); err == nil {
			vlog.VI(2).Infof("resolveAMT %s from cache -> %v", name, convertServersToStrings(ne.Servers, ne.Name))
			return &ne, nil
		}
		// Not in cache, call the real server.
		callCtx, _ := context.WithTimeout(ctx, callTimeout)
		call, err := client.StartCall(callCtx, name, "ResolveStep", nil, opts...)
		if err != nil {
			finalErr = err
			vlog.VI(2).Infof("ResolveStep.StartCall %s failed: %s", name, err)
			continue
		}
		entry := new(naming.MountEntry)
		if err := call.Finish(entry); err != nil {
			// If any replica says the name doesn't exist, return that fact.
			if verror.Is(err, naming.ErrNoSuchName.ID) || verror.Is(err, naming.ErrNoSuchNameRoot.ID) {
				return nil, err
			}
			// Keep track of the final error and continue with next server.
			finalErr = err
			vlog.VI(2).Infof("ResolveStep %s failed: %s", name, err)
			continue
		}
		// Add result to cache.
		ns.resolutionCache.remember(name, entry)
		vlog.VI(2).Infof("resolveAMT %s -> %v", name, entry)
		return entry, nil
	}
	return nil, finalErr
}

func terminal(e *naming.MountEntry) bool {
	return len(e.Name) == 0
}

// Resolve implements v.io/v23/naming.Namespace.
func (ns *namespace) Resolve(ctx *context.T, name string, opts ...naming.ResolveOpt) (*naming.MountEntry, error) {
	defer vlog.LogCall()()
	e, objPattern, _ := ns.rootMountEntry(name, opts...)
	if vlog.V(2) {
		_, file, line, _ := runtime.Caller(1)
		vlog.Infof("Resolve(%s) called from %s:%d", name, file, line)
		vlog.Infof("Resolve(%s) -> rootMountEntry %v", name, *e)
	}
	if skipResolve(opts) {
		setBlessingPatterns(e, objPattern)
		return e, nil
	}
	if len(e.Servers) == 0 {
		return nil, verror.New(naming.ErrNoSuchName, ctx, name)
	}
	client := v23.GetClient(ctx)
	callOpts := getCallOpts(opts)

	// Iterate walking through mount table servers.
	for remaining := ns.maxResolveDepth; remaining > 0; remaining-- {
		vlog.VI(2).Infof("Resolve(%s) loop %v", name, *e)
		if !e.ServesMountTable || terminal(e) {
			setBlessingPatterns(e, objPattern)
			vlog.VI(1).Infof("Resolve(%s) -> %v", name, *e)
			return e, nil
		}
		var err error
		curr := e
		if e, err = ns.resolveAgainstMountTable(ctx, client, curr, callOpts...); err != nil {
			// Lots of reasons why another error can happen.  We are trying
			// to single out "this isn't a mount table".
			if notAnMT(err) {
				setBlessingPatterns(curr, objPattern)
				vlog.VI(1).Infof("Resolve(%s) -> %v", name, curr)
				return curr, nil
			}
			if verror.Is(err, naming.ErrNoSuchNameRoot.ID) {
				err = verror.New(naming.ErrNoSuchName, ctx, name)
			}
			vlog.VI(1).Infof("Resolve(%s) -> (%s: %v)", err, name, curr)
			return nil, err
		}
	}
	return nil, verror.New(naming.ErrResolutionDepthExceeded, ctx)
}

// ResolveToMountTable implements v.io/v23/naming.Namespace.
func (ns *namespace) ResolveToMountTable(ctx *context.T, name string, opts ...naming.ResolveOpt) (*naming.MountEntry, error) {
	defer vlog.LogCall()()
	e, _, _ := ns.rootMountEntry(name, opts...)
	if vlog.V(2) {
		_, file, line, _ := runtime.Caller(1)
		vlog.Infof("ResolveToMountTable(%s) called from %s:%d", name, file, line)
		vlog.Infof("ResolveToMountTable(%s) -> rootNames %v", name, e)
	}
	if len(e.Servers) == 0 {
		return nil, verror.New(naming.ErrNoMountTable, ctx)
	}
	callOpts := getCallOpts(opts)
	client := v23.GetClient(ctx)
	last := e
	for remaining := ns.maxResolveDepth; remaining > 0; remaining-- {
		vlog.VI(2).Infof("ResolveToMountTable(%s) loop %v", name, e)
		var err error
		curr := e
		// If the next name to resolve doesn't point to a mount table, we're done.
		if !e.ServesMountTable || terminal(e) {
			vlog.VI(1).Infof("ResolveToMountTable(%s) -> %v", name, last)
			return last, nil
		}
		if e, err = ns.resolveAgainstMountTable(ctx, client, e, callOpts...); err != nil {
			if verror.Is(err, naming.ErrNoSuchNameRoot.ID) {
				vlog.VI(1).Infof("ResolveToMountTable(%s) -> %v (NoSuchRoot: %v)", name, last, curr)
				return last, nil
			}
			if verror.Is(err, naming.ErrNoSuchName.ID) {
				vlog.VI(1).Infof("ResolveToMountTable(%s) -> %v (NoSuchName: %v)", name, curr, curr)
				return curr, nil
			}
			// Lots of reasons why another error can happen.  We are trying
			// to single out "this isn't a mount table".
			if notAnMT(err) {
				vlog.VI(1).Infof("ResolveToMountTable(%s) -> %v", name, last)
				return last, nil
			}
			// TODO(caprita): If the server is unreachable for
			// example, we may still want to return its parent
			// mounttable rather than an error.
			vlog.VI(1).Infof("ResolveToMountTable(%s) -> %v", name, err)
			return nil, err
		}
		last = curr
	}
	return nil, verror.New(naming.ErrResolutionDepthExceeded, ctx)
}

// FlushCache flushes the most specific entry found for name.  It returns true if anything was
// actually flushed.
func (ns *namespace) FlushCacheEntry(name string) bool {
	defer vlog.LogCall()()
	flushed := false
	for _, n := range ns.rootName(name) {
		// Walk the cache as we would in a resolution.  Unlike a resolution, we have to follow
		// all branches since we want to flush all entries at which we might end up whereas in a resolution,
		// we stop with the first branch that works.
		if e, err := ns.resolutionCache.lookup(n); err == nil {
			// Recurse.
			for _, s := range e.Servers {
				flushed = flushed || ns.FlushCacheEntry(naming.Join(s.Server, e.Name))
			}
			if !flushed {
				// Forget the entry we just used.
				ns.resolutionCache.forget([]string{naming.TrimSuffix(n, e.Name)})
				flushed = true
			}
		}
	}
	return flushed
}

func skipResolve(opts []naming.ResolveOpt) bool {
	for _, o := range opts {
		if _, ok := o.(options.NoResolve); ok {
			return true
		}
	}
	return false
}

func getCallOpts(opts []naming.ResolveOpt) []rpc.CallOpt {
	var out []rpc.CallOpt
	for _, o := range opts {
		if co, ok := o.(rpc.CallOpt); ok {
			out = append(out, co)
		}
	}
	return out
}

// setBlessingPatterns overrides e.Servers.BlessingPatterns with p if p is
// non-empty. This will typically be the case for end servers (i.e., not
// mounttables) where the client explicitly specified a blessing pattern and
// thus explicitly chose to ignore the patterns from the MountEntry.
func setBlessingPatterns(e *naming.MountEntry, p security.BlessingPattern) {
	if len(p) == 0 {
		return
	}
	slice := []string{string(p)}
	for idx, _ := range e.Servers {
		e.Servers[idx].BlessingPatterns = slice
	}
}
