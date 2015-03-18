package namespace

import (
	"sync"
	"time"

	inaming "v.io/x/ref/profiles/internal/naming"

	"v.io/v23/naming"
	"v.io/v23/security"
	vdltime "v.io/v23/vdlroot/time"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

const defaultMaxResolveDepth = 32
const defaultMaxRecursiveGlobDepth = 10

const pkgPath = "v.io/x/ref/profiles/internal/naming/namespace"

var (
	errNotRootedName = verror.Register(pkgPath+".errNotRootedName", verror.NoRetry, "{1:}{2:} At least one root is not a rooted name{:_}")
)

// namespace is an implementation of naming.Namespace.
type namespace struct {
	sync.RWMutex

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

func badRoots(roots []string) error {
	return verror.New(errNotRootedName, nil, roots)
}

// Create a new namespace.
func New(roots ...string) (*namespace, error) {
	if !rooted(roots) {
		return nil, badRoots(roots)
	}
	// A namespace with no roots can still be used for lookups of rooted names.
	return &namespace{
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
	name = naming.Clean(name)
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
//
// Returns:
// (1) MountEntry
// (2) The BlessingPattern that the end servers are expected to match
//     (empty string if no such pattern).
// (3) Whether "name" is a rooted name or not (if not, the namespace roots
//     configured in "ns" will be used).
func (ns *namespace) rootMountEntry(name string, opts ...naming.ResolveOpt) (*naming.MountEntry, security.BlessingPattern, bool) {
	name = naming.Clean(name)
	objPattern, name := security.SplitPatternName(name)
	mtPattern := getRootPattern(opts)
	e := new(naming.MountEntry)
	deadline := vdltime.Deadline{time.Now().Add(time.Hour)} // plenty of time for a call
	address, suffix := naming.SplitAddressName(name)
	if len(address) == 0 {
		e.ServesMountTable = true
		e.Name = name
		ns.RLock()
		defer ns.RUnlock()
		for _, r := range ns.roots {
			// TODO(ashankar): Configured namespace roots should also include the pattern?
			server := naming.MountedServer{Server: r, Deadline: deadline}
			if len(mtPattern) > 0 {
				server.BlessingPatterns = []string{mtPattern}
			}
			e.Servers = append(e.Servers, server)
		}
		return e, objPattern, false
	}
	servesMT := true
	if ep, err := inaming.NewEndpoint(address); err == nil {
		servesMT = ep.ServesMountTable()
	}
	e.ServesMountTable = servesMT
	e.Name = suffix
	server := naming.MountedServer{Server: naming.JoinAddressName(address, ""), Deadline: deadline}
	if servesMT && len(mtPattern) > 0 {
		server.BlessingPatterns = []string{string(mtPattern)}
	}
	e.Servers = []naming.MountedServer{server}
	return e, objPattern, true
}

// notAnMT returns true if the error indicates this isn't a mounttable server.
func notAnMT(err error) bool {
	switch verror.ErrorID(err) {
	case verror.ErrBadArg.ID:
		// This should cover "rpc: wrong number of in-args".
		return true
	case verror.ErrNoExist.ID:
		// This should cover "rpc: unknown method", "rpc: dispatcher not
		// found", and dispatcher Lookup not found errors.
		return true
	case verror.ErrBadProtocol.ID:
		// This covers "rpc: response decoding failed: EOF".
		return true
	}
	return false
}

// all operations against the mount table service use this fixed timeout for the
// time being.
const callTimeout = 30 * time.Second

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

func getRootPattern(opts []naming.ResolveOpt) string {
	for _, opt := range opts {
		if pattern, ok := opt.(naming.RootBlessingPatternOpt); ok {
			return string(pattern)
		}
	}
	return ""
}
