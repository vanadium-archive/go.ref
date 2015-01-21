package naming

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	vnamespace "v.io/core/veyron/runtimes/google/naming/namespace"
)

// NewSimpleNamespace returns a simple implementation of a Namespace
// server for use in tests.  In particular, it ignores TTLs and not
// allow fully overlapping mount names.
func NewSimpleNamespace() naming.Namespace {
	ns, err := vnamespace.New()
	if err != nil {
		panic(err)
	}
	return &namespace{mounts: make(map[string][]string), ns: ns}
}

// namespace is a simple partial implementation of naming.Namespace.
type namespace struct {
	sync.Mutex
	mounts map[string][]string
	ns     naming.Namespace
}

func (ns *namespace) Mount(ctx *context.T, name, server string, _ time.Duration, _ ...naming.MountOpt) error {
	defer vlog.LogCall()()
	ns.Lock()
	defer ns.Unlock()
	for n, _ := range ns.mounts {
		if n != name && (strings.HasPrefix(name, n) || strings.HasPrefix(n, name)) {
			return fmt.Errorf("simple mount table does not allow names that are a prefix of each other")
		}
	}
	ns.mounts[name] = append(ns.mounts[name], server)
	return nil
}

func (ns *namespace) Unmount(ctx *context.T, name, server string) error {
	defer vlog.LogCall()()
	var servers []string
	ns.Lock()
	defer ns.Unlock()
	for _, s := range ns.mounts[name] {
		// When server is "", we remove all servers under name.
		if len(server) > 0 && s != server {
			servers = append(servers, s)
		}
	}
	if len(servers) > 0 {
		ns.mounts[name] = servers
	} else {
		delete(ns.mounts, name)
	}
	return nil
}

func (ns *namespace) Resolve(ctx *context.T, name string, opts ...naming.ResolveOpt) (*naming.MountEntry, error) {
	defer vlog.LogCall()()
	e, err := ns.ns.Resolve(ctx, name, options.NoResolve{})
	if err != nil {
		return e, err
	}
	if len(e.Servers) > 0 {
		e.Servers[0].Server = naming.JoinAddressName(e.Servers[0].Server, e.Name)
		e.Name = ""
		return e, nil
	}
	ns.Lock()
	defer ns.Unlock()
	name = e.Name
	for prefix, servers := range ns.mounts {
		if strings.HasPrefix(name, prefix) {
			e.Name = strings.TrimLeft(strings.TrimPrefix(name, prefix), "/")
			for _, s := range servers {
				e.Servers = append(e.Servers, naming.MountedServer{Server: s, Expires: time.Now().Add(1000 * time.Hour)})
			}
			return e, nil
		}
	}
	return nil, verror.Make(naming.ErrNoSuchName, ctx, fmt.Sprintf("Resolve name %q not found in %v", name, ns.mounts))
}

func (ns *namespace) ResolveToMountTable(ctx *context.T, name string, opts ...naming.ResolveOpt) (*naming.MountEntry, error) {
	defer vlog.LogCall()()
	// TODO(mattr): Implement this method for tests that might need it.
	panic("ResolveToMountTable not implemented")
	return nil, nil
}

func (ns *namespace) FlushCacheEntry(name string) bool {
	defer vlog.LogCall()()
	return false
}

func (ns *namespace) CacheCtl(ctls ...naming.CacheCtl) []naming.CacheCtl {
	defer vlog.LogCall()()
	return nil
}

func (ns *namespace) Glob(ctx *context.T, pattern string) (chan naming.MountEntry, error) {
	defer vlog.LogCall()()
	// TODO(mattr): Implement this method for tests that might need it.
	panic("Glob not implemented")
	return nil, nil
}

func (ns *namespace) SetRoots(...string) error {
	defer vlog.LogCall()()
	panic("Calling SetRoots on a mock namespace.  This is not supported.")
	return nil
}

func (ns *namespace) Roots() []string {
	defer vlog.LogCall()()
	panic("Calling Roots on a mock namespace.  This is not supported.")
	return nil
}
