package naming

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	vnamespace "v.io/x/ref/profiles/internal/naming/namespace"
)

// NewSimpleNamespace returns a simple implementation of a Namespace
// server for use in tests.  In particular, it ignores TTLs and not
// allow fully overlapping mount names.
func NewSimpleNamespace() ns.Namespace {
	ns, err := vnamespace.New()
	if err != nil {
		panic(err)
	}
	return &namespace{mounts: make(map[string]*naming.MountEntry), ns: ns}
}

// namespace is a simple partial implementation of ns.Namespace.
type namespace struct {
	sync.Mutex
	mounts map[string]*naming.MountEntry
	ns     ns.Namespace
}

func (ns *namespace) Mount(ctx *context.T, name, server string, _ time.Duration, opts ...naming.MountOpt) error {
	defer vlog.LogCall()()
	// TODO(ashankar,p): There is a bunch of processing in the real
	// namespace that is missing from this mock implementation and some of
	// it is duplicated here.  Figure out a way to share more code with the
	// real implementation?
	var blessingpatterns []string
	for _, o := range opts {
		if v, ok := o.(naming.MountedServerBlessingsOpt); ok {
			blessingpatterns = v
		}
	}
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
	s := naming.MountedServer{
		Server:           server,
		BlessingPatterns: blessingpatterns,
	}
	e.Servers = append(e.Servers, s)
	return nil
}

func (ns *namespace) Unmount(ctx *context.T, name, server string) error {
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

func (ns *namespace) Resolve(ctx *context.T, name string, opts ...naming.ResolveOpt) (*naming.MountEntry, error) {
	defer vlog.LogCall()()
	p, n := vnamespace.InternalSplitObjectName(name)
	var blessingpatterns []string
	if len(p) > 0 {
		blessingpatterns = []string{string(p)}
	}
	name = n
	if address, suffix := naming.SplitAddressName(name); len(address) > 0 {
		return &naming.MountEntry{
			Name: suffix,
			Servers: []naming.MountedServer{
				{Server: address, BlessingPatterns: blessingpatterns},
			},
		}, nil
	}
	ns.Lock()
	defer ns.Unlock()
	for prefix, e := range ns.mounts {
		if strings.HasPrefix(name, prefix) {
			ret := *e
			ret.Name = strings.TrimLeft(strings.TrimPrefix(name, prefix), "/")
			if len(blessingpatterns) > 0 {
				// Replace the blessing patterns with p.
				for idx, _ := range ret.Servers {
					ret.Servers[idx].BlessingPatterns = blessingpatterns
				}
			}
			return &ret, nil
		}
	}
	return nil, verror.New(naming.ErrNoSuchName, ctx, fmt.Sprintf("Resolve name %q not found in %v", name, ns.mounts))
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

func (ns *namespace) Glob(ctx *context.T, pattern string) (chan interface{}, error) {
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

func (ns *namespace) GetACL(ctx *context.T, name string) (acl access.TaggedACLMap, etag string, err error) {
	defer vlog.LogCall()()
	panic("Calling GetACL on a mock namespace.  This is not supported.")
	return nil, "", nil
}

func (ns *namespace) SetACL(ctx *context.T, name string, acl access.TaggedACLMap, etag string) error {
	defer vlog.LogCall()()
	panic("Calling SetACL on a mock namespace.  This is not supported.")
	return nil
}
