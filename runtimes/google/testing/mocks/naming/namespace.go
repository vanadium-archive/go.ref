package naming

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

// NewSimpleNamespace returns a simple implementation of a Namespace
// server for use in tests.  In particular, it ignores TTLs and not
// allow fully overlapping mount names.
func NewSimpleNamespace() naming.Namespace {
	return &namespace{mounts: make(map[string][]string)}
}

// namespace is a simple partial implementation of naming.Namespace.
type namespace struct {
	sync.Mutex
	mounts map[string][]string
}

func (ns *namespace) Mount(ctx context.T, name, server string, _ time.Duration) error {
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

func (ns *namespace) Unmount(ctx context.T, name, server string) error {
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

func (ns *namespace) Resolve(ctx context.T, name string) ([]string, error) {
	defer vlog.LogCall()()
	if address, _ := naming.SplitAddressName(name); len(address) > 0 {
		return []string{name}, nil
	}
	ns.Lock()
	defer ns.Unlock()
	for prefix, servers := range ns.mounts {
		if strings.HasPrefix(name, prefix) {
			suffix := strings.TrimLeft(strings.TrimPrefix(name, prefix), "/")
			var ret []string
			for _, s := range servers {
				ret = append(ret, naming.Join(s, suffix))
			}
			return ret, nil
		}
	}
	return nil, verror.NoExistf("Resolve name %q not found in %v", name, ns.mounts)
}

func (ns *namespace) ResolveToMountTable(ctx context.T, name string) ([]string, error) {
	defer vlog.LogCall()()
	// TODO(mattr): Implement this method for tests that might need it.
	panic("ResolveToMountTable not implemented")
	return nil, nil
}

func (ns *namespace) Unresolve(ctx context.T, name string) ([]string, error) {
	defer vlog.LogCall()()
	// TODO(mattr): Implement this method for tests that might need it.
	panic("Unresolve not implemented")
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

func (ns *namespace) Glob(ctx context.T, pattern string) (chan naming.MountEntry, error) {
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
