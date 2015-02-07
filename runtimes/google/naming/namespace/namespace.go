package namespace

import (
	"regexp"
	"sync"
	"time"

	inaming "v.io/core/veyron/runtimes/google/naming"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"
)

const defaultMaxResolveDepth = 32
const defaultMaxRecursiveGlobDepth = 10

var serverPatternRegexp = regexp.MustCompile("^\\[([^\\]]+)\\](.*)")

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

func badRoots(roots []string) verror.E {
	return verror.BadArgf("At least one root is not a rooted name: %q", roots)
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
func (ns *namespace) rootMountEntry(name string) (*naming.MountEntry, bool) {
	name = naming.Clean(name)
	_, objPattern, name := splitObjectName(name)
	e := new(naming.MountEntry)
	e.Pattern = string(objPattern)
	expiration := time.Now().Add(time.Hour) // plenty of time for a call
	address, suffix := naming.SplitAddressName(name)
	if len(address) == 0 {
		e.SetServesMountTable(true)
		e.Name = name
		ns.RLock()
		defer ns.RUnlock()
		for _, r := range ns.roots {
			e.Servers = append(e.Servers, naming.MountedServer{Server: r, Expires: expiration})
		}
		return e, false
	}
	servesMT := true
	if ep, err := inaming.NewEndpoint(address); err == nil {
		servesMT = ep.ServesMountTable()
	}
	e.SetServesMountTable(servesMT)
	e.Name = suffix
	e.Servers = append(e.Servers, naming.MountedServer{Server: naming.JoinAddressName(address, ""), Expires: expiration})
	return e, true
}

// notAnMT returns true if the error indicates this isn't a mounttable server.
func notAnMT(err error) bool {
	switch verror2.ErrorID(err) {
	case verror2.BadArg.ID:
		// This should cover "ipc: wrong number of in-args".
		return true
	case verror2.NoExist.ID:
		// This should cover "ipc: unknown method", "ipc: dispatcher not
		// found", and dispatcher Lookup not found errors.
		return true
	case verror2.BadProtocol.ID:
		// This covers "ipc: response decoding failed: EOF".
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

func splitObjectName(name string) (mtPattern, serverPattern security.BlessingPattern, objectName string) {
	objectName = name
	match := serverPatternRegexp.FindSubmatch([]byte(name))
	if match != nil {
		objectName = string(match[2])
		if naming.Rooted(objectName) {
			mtPattern = security.BlessingPattern(match[1])
		} else {
			serverPattern = security.BlessingPattern(match[1])
			return
		}
	}
	if !naming.Rooted(objectName) {
		return
	}

	address, relative := naming.SplitAddressName(objectName)
	match = serverPatternRegexp.FindSubmatch([]byte(relative))
	if match != nil {
		serverPattern = security.BlessingPattern(match[1])
		objectName = naming.JoinAddressName(address, string(match[2]))
	}
	return
}
