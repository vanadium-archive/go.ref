package namespace

import (
	"errors"
	"runtime"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

func convertServersToStrings(servers []mountedServer, suffix string) (ret []string) {
	for _, s := range servers {
		ret = append(ret, naming.Join(s.Server, suffix))
	}
	return
}

func (ns *namespace) resolveAgainstMountTable(ctx context.T, client ipc.Client, names []string) ([]string, error) {
	// Try each server till one answers.
	finalErr := errors.New("no servers to resolve query")
	for _, name := range names {
		// We want to resolve the name against the MountTable specified in its
		// address, without recursing through ourselves. To this we force
		// the entire name component to be terminal.
		name = naming.MakeTerminal(name)
		// First check the cache.
		if servers, suffix := ns.resolutionCache.lookup(name); len(servers) > 0 {
			return convertServersToStrings(servers, suffix), nil
		}
		// Not in cache, call the real server.
		callCtx, _ := ctx.WithTimeout(callTimeout)
		call, err := client.StartCall(callCtx, name, "ResolveStep", nil)
		if err != nil {
			finalErr = err
			vlog.VI(2).Infof("ResolveStep.StartCall %s failed: %s", name, err)
			continue
		}
		servers := []mountedServer{}
		var suffix string
		ierr := call.Finish(&servers, &suffix, &err)
		if ierr != nil {
			// Internal/system error.
			finalErr = ierr
			vlog.VI(2).Infof("ResolveStep.Finish %s failed: %s", name, ierr)
			continue
		}
		// If any replica says the name doesn't exist, return that fact.
		if err != nil {
			if verror.Equal(naming.ErrNoSuchName, err) || verror.Equal(naming.ErrNoSuchNameRoot, err) {
				return nil, err
			}
			finalErr = err
			vlog.VI(2).Infof("ResolveStep %s failed: %s", name, err)
			continue
		}
		// Add result to cache.
		ns.resolutionCache.remember(naming.TrimSuffix(name, suffix), servers)
		return convertServersToStrings(servers, suffix), nil
	}
	return nil, finalErr
}

func terminal(names []string) bool {
	for _, name := range names {
		if !naming.Terminal(name) {
			return false
		}
	}
	return true
}

func makeTerminal(names []string) (ret []string) {
	for _, name := range names {
		ret = append(ret, naming.MakeTerminal(name))
	}
	return
}

// Resolve implements veyron2/naming.Namespace.
func (ns *namespace) Resolve(ctx context.T, name string) ([]string, error) {
	defer vlog.LogCall()()
	names := ns.rootName(name)
	if vlog.V(2) {
		_, file, line, _ := runtime.Caller(1)
		vlog.Infof("Resolve(%s) called from %s:%d", name, file, line)
		vlog.Infof("Resolve(%s) -> rootNames %s", name, names)
	}
	if len(names) == 0 {
		return nil, naming.ErrNoMountTable
	}
	// Iterate walking through mount table servers.
	for remaining := ns.maxResolveDepth; remaining > 0; remaining-- {
		vlog.VI(2).Infof("Resolve(%s) loop %s", name, names)
		if terminal(names) {
			vlog.VI(1).Infof("Resolve(%s) -> %s", name, names)
			return names, nil
		}
		var err error
		curr := names
		if names, err = ns.resolveAgainstMountTable(ctx, ns.rt.Client(), names); err != nil {
			// If the name could not be found in the mount table, return an error.
			if verror.Equal(naming.ErrNoSuchNameRoot, err) {
				err = naming.ErrNoSuchName
			}
			if verror.Equal(naming.ErrNoSuchName, err) {
				vlog.VI(1).Infof("Resolve(%s) -> (NoSuchName: %v)", name, curr)
				return nil, err
			}
			// Any other failure (server not found, no ResolveStep
			// method, etc.) are a sign that iterative resolution can
			// stop.
			t := makeTerminal(curr)
			vlog.VI(1).Infof("Resolve(%s) -> %s", name, t)
			return t, nil
		}
	}
	return nil, naming.ErrResolutionDepthExceeded
}

// ResolveToMountTable implements veyron2/naming.Namespace.
func (ns *namespace) ResolveToMountTable(ctx context.T, name string) ([]string, error) {
	defer vlog.LogCall()()
	names := ns.rootName(name)
	if vlog.V(2) {
		_, file, line, _ := runtime.Caller(1)
		vlog.Infof("ResolveToMountTable(%s) called from %s:%d", name, file, line)
		vlog.Infof("ResolveToMountTable(%s) -> rootNames %s", name, names)
	}
	if len(names) == 0 {
		return nil, naming.ErrNoMountTable
	}
	last := names
	for remaining := ns.maxResolveDepth; remaining > 0; remaining-- {
		vlog.VI(2).Infof("ResolveToMountTable(%s) loop %s", name, names)
		var err error
		curr := names
		if terminal(curr) {
			t := makeTerminal(last)
			vlog.VI(1).Infof("ResolveToMountTable(%s) -> %s", name, t)
			return t, nil
		}
		if names, err = ns.resolveAgainstMountTable(ctx, ns.rt.Client(), names); err != nil {
			if verror.Equal(naming.ErrNoSuchNameRoot, err) {
				t := makeTerminal(last)
				vlog.VI(1).Infof("ResolveToMountTable(%s) -> %s (NoSuchRoot: %v)", name, t, curr)
				return t, nil
			}
			if verror.Equal(naming.ErrNoSuchName, err) {
				t := makeTerminal(curr)
				vlog.VI(1).Infof("ResolveToMountTable(%s) -> %s (NoSuchName: %v)", name, t, curr)
				return t, nil
			}
			// Lots of reasons why another error can happen.  We are trying
			// to single out "this isn't a mount table".
			// TODO(p); make this less of a hack, make a specific verror code
			// that means "we are up but don't implement what you are
			// asking for".
			if notAnMT(err) {
				t := makeTerminal(last)
				vlog.VI(1).Infof("ResolveToMountTable(%s) -> %s", name, t)
				return t, nil
			}
			// TODO(caprita): If the server is unreachable for
			// example, we may still want to return its parent
			// mounttable rather than an error.
			vlog.VI(1).Infof("ResolveToMountTable(%s) -> %v", name, err)
			return nil, err
		}

		last = curr
	}
	return nil, naming.ErrResolutionDepthExceeded
}

func finishUnresolve(call ipc.Call) ([]string, error) {
	var newNames []string
	var unresolveErr error
	if err := call.Finish(&newNames, &unresolveErr); err != nil {
		return nil, err
	}
	return newNames, unresolveErr
}

func unresolveAgainstServer(ctx context.T, client ipc.Client, names []string) ([]string, error) {
	finalErr := errors.New("no servers to unresolve")
	for _, name := range names {
		name = naming.MakeTerminal(name)
		callCtx, _ := ctx.WithTimeout(callTimeout)
		call, err := client.StartCall(callCtx, name, "UnresolveStep", nil)
		if err != nil {
			finalErr = err
			vlog.VI(2).Infof("StartCall %q.UnresolveStep() failed: %s", name, err)
			continue

		}
		newNames, err := finishUnresolve(call)
		if err == nil {
			return newNames, nil
		}
		finalErr = err
		vlog.VI(2).Infof("Finish %s failed: %v", name, err)
	}
	return nil, finalErr
}

// TODO(caprita): Unresolve currently picks the first responsive server as it
// goes up the ancestry line.  This means that, if a service has several
// ancestors (each with potentially more than one replica), the first responsive
// replica of the first responsive ancestor is preferred.  In particular,
// other branches are ignored.  We need to figure out a desired policy for
// selecting the right branch (or should we return a representative of all
// branches?).

// Unesolve implements veyron2/naming.Namespace.
func (ns *namespace) Unresolve(ctx context.T, name string) ([]string, error) {
	defer vlog.LogCall()()
	vlog.VI(2).Infof("Unresolve %s", name)
	names, err := ns.Resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	for remaining := ns.maxResolveDepth; remaining > 0; remaining-- {
		vlog.VI(2).Infof("Unresolve loop %s", names)
		curr := names
		if names, err = unresolveAgainstServer(ctx, ns.rt.Client(), names); err != nil {
			return nil, err
		}
		if len(names) == 0 {
			return curr, nil
		}
	}
	return nil, naming.ErrResolutionDepthExceeded
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
		n := naming.MakeTerminal(n)
		if mts, suffix := ns.resolutionCache.lookup(n); mts != nil {
			// Recurse.
			for _, server := range mts {
				flushed = flushed || ns.FlushCacheEntry(naming.Join(server.Server, suffix))
			}
			if !flushed {
				// Forget the entry we just used.
				ns.resolutionCache.forget([]string{naming.TrimSuffix(n, suffix)})
				flushed = true
			}
		}
	}
	return flushed
}
