package namespace

import (
	"sync"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

const defaultMaxResolveDepth = 32
const defaultMaxRecursiveGlobDepth = 10

// namespace is an implementation of naming.Namespace.
type namespace struct {
	sync.RWMutex
	rt veyron2.Runtime

	// the default root servers for resolutions in this namespace.
	roots []string

	// depth limits
	maxResolveDepth       int
	maxRecursiveGlobDepth int

	// cache for name resolutions
	resolutionCache cache
}

func rooted(names []string) bool {
	for _, n := range names {
		if a, _ := naming.SplitAddressName(n); len(a) == 0 {
			return false
		}
	}
	return true
}

func badRoots(roots []string) verror.E {
	return verror.BadArgf("At least one root is not a rooted name: %q", roots)
}

// Create a new namespace.
func New(rt veyron2.Runtime, roots ...string) (*namespace, error) {
	if !rooted(roots) {
		return nil, badRoots(roots)
	}
	// A namespace with no roots can still be used for lookups of rooted names.
	return &namespace{
		rt:                    rt,
		roots:                 roots,
		maxResolveDepth:       defaultMaxResolveDepth,
		maxRecursiveGlobDepth: defaultMaxRecursiveGlobDepth,
		resolutionCache:       newTTLCache(),
	}, nil
}

// SetRoots implements naming.Namespace.SetRoots
func (ns *namespace) SetRoots(roots ...string) error {
	defer vlog.LogCall()()
	// Allow roots to be cleared with a call of SetRoots()
	if len(roots) > 0 && !rooted(roots) {
		return badRoots(roots)
	}
	ns.Lock()
	defer ns.Unlock()
	// TODO(cnicolaou): filter out duplicate values.
	ns.roots = roots
	return nil
}

// SetDepthLimits overrides the default limits.
func (ns *namespace) SetDepthLimits(resolve, glob int) {
	if resolve >= 0 {
		ns.maxResolveDepth = resolve
	}
	if glob >= 0 {
		ns.maxRecursiveGlobDepth = glob
	}
}

// Roots implements naming.Namespace.Roots
func (ns *namespace) Roots() []string {
	//nologcall
	ns.RLock()
	defer ns.RUnlock()
	roots := make([]string, len(ns.roots))
	for i, r := range ns.roots {
		roots[i] = r
	}
	return roots
}

// rootName 'roots' a name: if name is not a rooted name, it prepends the root
// mounttable's OA.
func (ns *namespace) rootName(name string) []string {
	if address, _ := naming.SplitAddressName(name); len(address) == 0 {
		var ret []string
		ns.RLock()
		defer ns.RUnlock()
		for _, r := range ns.roots {
			ret = append(ret, naming.Join(r, name))
		}
		return ret
	}
	return []string{name}
}

// rootMountEntry 'roots' a name creating a mount entry for the name.
func (ns *namespace) rootMountEntry(name string) *naming.MountEntry {
	e := new(naming.MountEntry)
	expiration := time.Now().Add(time.Hour) // plenty of time for a call
	address, suffix := naming.SplitAddressName(name)
	if len(address) == 0 {
		e.MT = true
		e.Name = name
		ns.RLock()
		defer ns.RUnlock()
		for _, r := range ns.roots {
			e.Servers = append(e.Servers, naming.MountedServer{Server: r, Expires: expiration})
		}
		return e
	}
	// TODO(p): right now I assume any address handed to me to be resolved is a mount table.
	// Eventually we should do something like the following:
	// if ep, err := ns.rt.NewEndpoint(address); err == nil && ep.ServesMountTable() {
	//	e.MT = true
	// }
	e.MT = true
	e.Name = suffix
	e.Servers = append(e.Servers, naming.MountedServer{Server: address, Expires: expiration})
	return e
}

// notAnMT returns true if the error indicates this isn't a mounttable server.
func notAnMT(err error) bool {
	switch verror.ErrorID(err) {
	case verror.BadArg:
		// This should cover "ipc: wrong number of in-args".
		return true
	case verror.NoExist:
		// This should cover "ipc: unknown method", "ipc: dispatcher not
		// found", and "ipc: LeafDispatcher lookup on non-empty suffix".
		return true
	case verror.BadProtocol:
		// This covers "ipc: response decoding failed: EOF".
		return true
	}
	return false
}

// all operations against the mount table service use this fixed timeout for the
// time being.
const callTimeout = 10 * time.Second

// CacheCtl implements naming.Namespace.CacheCtl
func (ns *namespace) CacheCtl(ctls ...naming.CacheCtl) []naming.CacheCtl {
	defer vlog.LogCall()()
	for _, c := range ctls {
		switch v := c.(type) {
		case naming.DisableCache:
			ns.Lock()
			if _, isDisabled := ns.resolutionCache.(nullCache); isDisabled {
				if !v {
					ns.resolutionCache = newTTLCache()
				}
			} else {
				if v {
					ns.resolutionCache = newNullCache()
				}
			}
			ns.Unlock()
		}
	}
	ns.RLock()
	defer ns.RUnlock()
	if _, isDisabled := ns.resolutionCache.(nullCache); isDisabled {
		return []naming.CacheCtl{naming.DisableCache(true)}
	}
	return nil
}
