package mounttable

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"veyron/lib/glob"
	vsecurity "veyron/security"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mounttable"
	"veyron2/verror"
	"veyron2/vlog"
)

var (
	errNamingLoop = verror.Make(verror.BadArg, "Loop in namespace")
)

// mountTable represents a namespace.  One exists per server instance.
type mountTable struct {
	sync.RWMutex
	root *node
	acls map[string]security.Authorizer
}

// mountContext represents a client bind.  The name is the name that was bound to.
type mountContext struct {
	name         string
	elems        []string // parsed elements of name
	cleanedElems []string // parsed elements of cleaned name (with leading /
	// and double / removed).
	mt *mountTable
}

// mount represents a single mount point.  It contains OAs of all servers mounted
// here.  The servers are considered equivalent, i.e., RPCs to a name below this
// point can be sent to any of these servers.
type mount struct {
	servers *serverList
}

// node is a single point in the tree representing the mount table.
type node struct {
	parent   *node
	mount    *mount
	children map[string]*node
}

// NewMountTable creates a new server that uses the default authorization policy.
func NewMountTable(aclfile string) (*mountTable, error) {
	acls, err := parseACLs(aclfile)
	if err != nil {
		return nil, err
	}
	return &mountTable{
		root: new(node),
		acls: acls,
	}, nil
}

func parseACLs(path string) (map[string]security.Authorizer, error) {
	if path == "" {
		return nil, nil
	}
	var acls map[string]security.ACL
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err = json.NewDecoder(f).Decode(&acls); err != nil {
		return nil, err
	}
	result := make(map[string]security.Authorizer)
	for name, acl := range acls {
		result[name] = vsecurity.NewACLAuthorizer(acl)
	}
	if result["/"] == nil {
		return nil, fmt.Errorf("No acl for / in %s", path)
	}
	return result, nil
}

// LookupServer implements ipc.Dispatcher.Lookup.
func (mt *mountTable) Lookup(name, method string) (ipc.Invoker, security.Authorizer, error) {
	vlog.VI(2).Infof("*********************Lookup %s", name)
	mt.RLock()
	defer mt.RUnlock()
	ms := &mountContext{
		name: name,
		mt:   mt,
	}
	if len(name) > 0 {
		ms.elems = strings.Split(name, "/")
		ms.cleanedElems = strings.Split(strings.TrimLeft(path.Clean(name), "/"), "/")
	}
	return ipc.ReflectInvoker(mounttable.NewServerMountTable(ms)), ms, nil
}

// findNode returns the node for the name path represented by elems.  If none exists and create is false, return nil.
// Otherwise create the path and return a pointer to the terminal node.
func (mt *mountTable) findNode(elems []string, create bool) *node {
	cur := mt.root

	// Iterate down the tree.
	for _, e := range elems {
		// if we hit another mount table, we're done
		if cur.mount != nil {
			return nil
		}
		// then walk the children
		c, ok := cur.children[e]
		if ok {
			cur = c
			continue
		}
		if !create {
			return nil
		}
		next := new(node)
		if cur.children == nil {
			cur.children = make(map[string]*node)
		}
		cur.children[e] = next
		next.parent = cur
		cur = next
	}
	return cur
}

// isActive returns true if a mount has unexpired servers attached.
func (m *mount) isActive() bool {
	if m == nil {
		return false
	}
	return m.servers.removeExpired() > 0
}

// walk returns the first mount point node on the elems path and the suffix of elems below that mount point.
// If no mount point is found, it returns nil,nil.
func (mt *mountTable) walk(n *node, elems []string) (*node, []string) {
	if n.mount.isActive() {
		return n, elems
	} else if n.mount != nil {
		n.removeUseless()
	}
	if len(elems) > 0 {
		if c, ok := n.children[elems[0]]; ok {
			if nn, nelems := mt.walk(c, elems[1:]); nn != nil {
				return nn, nelems
			}
		}
	}
	return nil, nil
}

func (mt *mountTable) authorizeStep(name string, c security.Context) error {
	if mt.acls == nil {
		return nil
	}
	mt.Lock()
	acl := mt.acls[name]
	mt.Unlock()
	vlog.VI(2).Infof("authorizeStep(%s) %s %s %s", name, c.RemoteID(), c.Label(), acl)
	if acl != nil {
		return acl.Authorize(c)
	}
	return nil
}

func slashSlashJoin(elems []string) string {
	if len(elems) == 2 && len(elems[0]) == 0 && len(elems[1]) == 0 {
		return "//"
	}
	if len(elems) > 0 && len(elems[0]) == 0 {
		return "/" + strings.Join(elems, "/")
	}
	return strings.Join(elems, "/")
}

// Authorize verifies that the client has access to the requested node.
// Checks the acls on all nodes in the path starting at the root.
func (ms *mountContext) Authorize(context security.Context) error {
	if err := ms.mt.authorizeStep("/", context); err != nil {
		return err
	}
	key := ""
	for _, step := range ms.cleanedElems {
		key := key + "/" + step
		if err := ms.mt.authorizeStep(key, context); err != nil {
			return err
		}
	}
	return nil
}

// ResolveStep returns the next server in a resolution, the name remaining below that server,
// and whether or not that server is another mount table.
func (ms *mountContext) ResolveStep(context ipc.ServerContext) (servers []mounttable.MountedServer, suffix string, err error) {
	vlog.VI(2).Infof("ResolveStep %q", ms.name)
	mt := ms.mt
	// TODO(caprita): we need to grab a write lock because walk may
	// garbage-collect expired servers.  Rework this to avoid this potential
	// bottleneck.
	mt.Lock()
	defer mt.Unlock()
	// Find the next mount point for the name.
	n, elems := mt.walk(mt.root, ms.elems)
	if n == nil {
		if len(ms.elems) == 0 {
			return nil, ms.name, naming.ErrNoSuchNameRoot
		}
		return nil, ms.name, naming.ErrNoSuchName
	}
	return n.mount.servers.copyToSlice(), slashSlashJoin(elems), nil
}

// Mount a server onto the name in the receiver.
func (ms *mountContext) Mount(context ipc.ServerContext, server string, ttlsecs uint32) error {
	mt := ms.mt
	if ttlsecs == 0 {
		ttlsecs = 10 * 365 * 24 * 60 * 60 // a really long time
	}
	vlog.VI(2).Infof("*********************Mount %q -> %s", ms.name, server)

	// Make sure the server name is reasonable.
	epString, _ := naming.SplitAddressName(server)
	if _, err := rt.R().NewEndpoint(epString); err != nil {
		return fmt.Errorf("malformed address %q for mounted server %q", epString, server)
	}

	// Find/create node in namespace and add the mount.
	mt.Lock()
	defer mt.Unlock()
	n := mt.findNode(ms.cleanedElems, true)
	if n == nil {
		return naming.ErrNoSuchName
	}
	if n.mount == nil {
		n.mount = &mount{
			servers: NewServerList(),
		}
	}
	m := n.mount
	m.servers.add(server, time.Duration(ttlsecs)*time.Second)
	return nil
}

// A useful node has children or an active mount.
func (n *node) isUseful() bool {
	return len(n.children) > 0 || n.mount.isActive()
}

// removeUseless removes a node and all of its ascendants that are not useful.
func (n *node) removeUseless() {
	if n.isUseful() {
		return
	}
	if n.parent == nil {
		return
	}
	for k, c := range n.parent.children {
		if c == n {
			delete(n.parent.children, k)
			break
		}
	}
	n.parent.removeUseless()
}

// removeUselessSubtree removes all descendant nodes of this node that are not
// useful (after calling removeUselessSubtree recursively).  Returns if this
// node is useful anymore.
func (n *node) removeUselessSubtree() bool {
	for k, c := range n.children {
		if !c.removeUselessSubtree() {
			delete(n.children, k)
		}
	}
	return n.isUseful()
}

// Unmount removes servers from the name in the receiver. If server is specified, only that
// server is removed.
func (ms *mountContext) Unmount(context ipc.ServerContext, server string) error {
	mt := ms.mt
	mt.Lock()
	defer mt.Unlock()
	n := mt.findNode(ms.cleanedElems, false)
	if n == nil {
		return nil
	}
	defer n.removeUseless()
	if server == "" {
		n.mount = nil
		return nil
	}
	if n.mount != nil && n.mount.servers.remove(server) == 0 {
		n.mount = nil
	}
	return nil
}

// A struct holding a partial result of Glob.
type globEntry struct {
	n    *node
	name string
}

func (mt *mountTable) globStep(n *node, name string, pattern *glob.Glob, context ipc.ServerContext, reply mounttable.GlobbableServiceGlobStream) {
	vlog.VI(2).Infof("globStep(%s, %s)", name, pattern)

	if mt.acls != nil {
		acl_name := "/" + strings.TrimLeft(naming.Join(context.Suffix(), name), "/")
		// Skip this node if the user isn't authorized.
		if acl := mt.acls[acl_name]; acl != nil {
			if err := acl.Authorize(context); err != nil {
				return
			}
		}
	}

	sender := reply.SendStream()
	// If this is a mount point, we're done.
	if m := n.mount; m != nil {
		// Garbage-collect if expired.
		if !m.isActive() {
			n.removeUseless()
			return
		}
		sender.Send(mounttable.MountEntry{Name: name, Servers: m.servers.copyToSlice()})
		return
	}

	if pattern.Len() == 0 {
		// Garbage-collect if no useful descendants.
		if !n.removeUselessSubtree() {
			n.removeUseless()
			return
		}
		sender.Send(mounttable.MountEntry{Name: name})
	}

	if pattern.Finished() {
		return
	}

	// Recurse through the children.
	for k, c := range n.children {
		if ok, suffix := pattern.MatchInitialSegment(k); ok {
			mt.globStep(c, naming.Join(name, k), suffix, context, reply)
		}
	}
}

// Glob finds matches in the namespace.  If we reach a mount point before matching the
// whole pattern, return that mount point.
// pattern is a glob pattern as defined by the veyron/lib/glob package.
func (ms *mountContext) Glob(context ipc.ServerContext, pattern string, reply mounttable.GlobbableServiceGlobStream) error {
	vlog.VI(2).Infof("mt.Glob %v", ms.elems)

	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}

	mt := ms.mt

	// TODO(caprita): we need to grab a write lock because globStep may
	// garbage-collect expired servers.  Rework this to avoid this potential
	// bottleneck.
	mt.Lock()
	defer mt.Unlock()

	// If we can't find the node corresponding to the current name then there are no results.
	n := mt.findNode(ms.cleanedElems, false)
	if n == nil {
		return nil
	}

	mt.globStep(n, "", g, context, reply)
	return nil
}
