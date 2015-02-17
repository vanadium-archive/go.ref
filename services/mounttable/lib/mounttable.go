package mounttable

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"v.io/core/veyron/lib/glob"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mounttable"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/vlog"
)

var (
	errNamingLoop = verror.Register("v.io/core/veyron/services/mountable/lib", verror.NoRetry, "Loop in namespace")
	traverseTags  = []mounttable.Tag{mounttable.Read, mounttable.Resolve, mounttable.Create, mounttable.Admin}
	createTags    = []mounttable.Tag{mounttable.Create, mounttable.Admin}
	removeTags    = []mounttable.Tag{mounttable.Admin}
	mountTags     = []mounttable.Tag{mounttable.Mount, mounttable.Admin}
	globTags      = []mounttable.Tag{mounttable.Read, mounttable.Admin}
	setTags       = []mounttable.Tag{mounttable.Admin}
	getTags       = []mounttable.Tag{mounttable.Admin, mounttable.Read}
	allTags       = []mounttable.Tag{mounttable.Read, mounttable.Resolve, mounttable.Admin, mounttable.Mount, mounttable.Create}
)

// mountTable represents a namespace.  One exists per server instance.
type mountTable struct {
	root       *node
	superUsers access.ACL
}

var _ ipc.Dispatcher = (*mountTable)(nil)

// mountContext represents a client bind.  The name is the name that was bound to.
type mountContext struct {
	name  string
	elems []string // parsed elements of name
	mt    *mountTable
}

// mount represents a single mount point.  It contains the rooted names of all servers mounted
// here.  The servers are considered equivalent, i.e., RPCs to a name below this
// point can be sent to any of these servers.
type mount struct {
	servers *serverList
	mt      bool
}

// node is a single point in the tree representing the mount table.
type node struct {
	sync.RWMutex
	parent       *node
	mount        *mount
	children     map[string]*node
	acls         *TAMG
	amTemplate   access.TaggedACLMap
	explicitACLs bool
}

const templateVar = "%%"

// NewMountTableDispatcher creates a new server that uses the ACLs specified in
// aclfile for authorization.
//
// aclfile is a JSON-encoded mapping from paths in the mounttable to the
// access.TaggedACLMap for that path. The tags used in the map are the typical
// access tags (the Tag type defined in veyron2/services/security/access).
func NewMountTableDispatcher(aclfile string) (ipc.Dispatcher, error) {
	mt := &mountTable{
		root: new(node),
	}
	mt.root.parent = new(node) // just for its lock
	if err := mt.parseACLs(aclfile); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return mt, nil
}

func (mt *mountTable) parseACLs(path string) error {
	vlog.VI(2).Infof("parseACLs(%s)", path)
	if path == "" {
		return nil
	}
	var tams map[string]access.TaggedACLMap
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = json.NewDecoder(f).Decode(&tams); err != nil {
		return err
	}
	for name, tam := range tams {
		var elems []string
		isPattern := false
		// Create name and add the ACL map to it.
		if len(name) == 0 {
			// If the config file has is an Admin tag on the root ACL, the
			// list of Admin users is the equivalent of a super user for
			// the whole table.  This is not updated if the ACL is later
			// modified.
			if acl, exists := tam[string(mounttable.Admin)]; exists {
				mt.superUsers = acl
			}
		} else {
			// ACL templates terminate with a %% element.  These are very
			// constrained matches, i.e., the trailing element of the name
			// is copied into every %% in the ACL.
			elems = strings.Split(name, "/")
			if elems[len(elems)-1] == templateVar {
				isPattern = true
				elems = elems[:len(elems)-1]
			}
		}

		n, err := mt.findNode(nil, elems, true, nil)
		if n != nil || err == nil {
			vlog.VI(2).Infof("added tam %v to %s", tam, name)
			if isPattern {
				n.amTemplate = tam
			} else {
				n.acls, _ = n.acls.Set("", tam)
				n.explicitACLs = true
			}
		}
		n.parent.Unlock()
		n.Unlock()
	}
	return nil
}

// Lookup implements ipc.Dispatcher.Lookup.
func (mt *mountTable) Lookup(name string) (interface{}, security.Authorizer, error) {
	vlog.VI(2).Infof("*********************Lookup %s", name)
	ms := &mountContext{
		name: name,
		mt:   mt,
	}
	if len(name) > 0 {
		ms.elems = strings.Split(name, "/")
	}
	return mounttable.MountTableServer(ms), ms, nil
}

// isActive returns true if a mount has unexpired servers attached.
func (m *mount) isActive() bool {
	if m == nil {
		return false
	}
	return m.servers.removeExpired() > 0
}

// satisfies returns no error if the ctx + n.acls satisfies the associated one of the required Tags.
func (n *node) satisfies(mt *mountTable, ctx ipc.ServerContext, tags []mounttable.Tag) error {
	// No ACLs means everything (for now).
	if ctx == nil || tags == nil || n.acls == nil {
		return nil
	}
	// "Self-RPCs" are always authorized.
	if l, r := ctx.LocalBlessings(), ctx.RemoteBlessings(); l != nil && r != nil && reflect.DeepEqual(l.PublicKey(), r.PublicKey()) {
		return nil
	}
	// Match client's blessings against the ACLs.
	var blessings []string
	var invalidB []security.RejectedBlessing
	if ctx.RemoteBlessings() != nil {
		blessings, invalidB = ctx.RemoteBlessings().ForContext(ctx)
	}
	for _, tag := range tags {
		if acl, exists := n.acls.GetACLForTag(string(tag)); exists && acl.Includes(blessings...) {
			return nil
		}
	}
	if mt.superUsers.Includes(blessings...) {
		return nil
	}
	if len(invalidB) > 0 {
		return verror.New(verror.ErrNoAccess, ctx.Context(), blessings, invalidB)
	}
	return verror.New(verror.ErrNoAccess, ctx.Context(), blessings)
}

func expand(acl *access.ACL, name string) *access.ACL {
	newACL := new(access.ACL)
	for _, bp := range acl.In {
		newACL.In = append(newACL.In, security.BlessingPattern(strings.Replace(string(bp), templateVar, name, -1)))
	}
	for _, bp := range acl.NotIn {
		newACL.NotIn = append(newACL.NotIn, strings.Replace(bp, templateVar, name, -1))
	}
	return newACL
}

// satisfiesTemplate returns no error if the ctx + n.amTemplate satisfies the associated one of
// the required Tags.
func (n *node) satisfiesTemplate(ctx ipc.ServerContext, tags []mounttable.Tag, name string) error {
	if n.amTemplate == nil {
		return nil
	}
	// Match client's blessings against the ACLs.
	var blessings []string
	var invalidB []security.RejectedBlessing
	if ctx.RemoteBlessings() != nil {
		blessings, invalidB = ctx.RemoteBlessings().ForContext(ctx)
	}
	for _, tag := range tags {
		if acl, exists := n.amTemplate[string(tag)]; exists && expand(&acl, name).Includes(blessings...) {
			return nil
		}
	}
	return verror.New(verror.ErrNoAccess, ctx.Context(), blessings, invalidB)
}

// copyACLs copies one nodes ACLs to another and adds the clients blessings as
// patterns to the Admin tag.
func copyACLs(ctx ipc.ServerContext, cur *node) *TAMG {
	if ctx == nil {
		return nil
	}
	if cur.acls == nil {
		return nil
	}
	acls := cur.acls.Copy()
	var blessings []string
	if ctx.RemoteBlessings() != nil {
		blessings, _ = ctx.RemoteBlessings().ForContext(ctx)
	}
	for _, b := range blessings {
		acls.Add(security.BlessingPattern(b), string(mounttable.Admin))
	}
	return acls
}

// createTAMGFromTemplate creates a new TAMG from the template subsituting name for %% everywhere.
func createTAMGFromTemplate(tam access.TaggedACLMap, name string) *TAMG {
	tamg := NewTAMG()
	for tag, acl := range tam {
		tamg.tam[tag] = *expand(&acl, name)
	}
	return tamg
}

// traverse returns the node for the path represented by elems.  If none exists and create is false, return nil.
// Otherwise create the path and return a pointer to the terminal node.  If a mount point is encountered
// while following the path, return that node and any remaining elems.
//
// If it returns a node, both the node and its parent are locked.
func (mt *mountTable) traverse(ctx ipc.ServerContext, elems []string, create bool) (*node, []string, error) {
	// Invariant is that the current node and its parent are both locked.
	cur := mt.root
	cur.parent.Lock()
	cur.Lock()
	for i, e := range elems {
		vlog.VI(2).Infof("satisfying %v %v", elems[0:i], *cur)
		if ctx != nil {
			if err := cur.satisfies(mt, ctx, traverseTags); err != nil {
				cur.parent.Unlock()
				cur.Unlock()
				return nil, nil, err
			}
		}
		// If we hit another mount table, we're done.
		if cur.mount.isActive() {
			return cur, elems[i:], nil
		}
		// Walk the children looking for a match.
		c, ok := cur.children[e]
		if ok {
			cur.parent.Unlock()
			cur = c
			cur.Lock()
			continue
		}
		if !create {
			cur.parent.Unlock()
			cur.Unlock()
			return nil, nil, nil
		}
		// Create a new node and keep recursing.
		cur.parent.Unlock()
		if err := cur.satisfies(mt, ctx, createTags); err != nil {
			cur.Unlock()
			return nil, nil, err
		}
		if ctx != nil {
			if err := cur.satisfiesTemplate(ctx, createTags, e); err != nil {
				cur.Unlock()
				return nil, nil, err
			}
		}
		// At this point cur is still locked, OK to use and change it.
		next := new(node)
		next.parent = cur
		if cur.amTemplate != nil {
			next.acls = createTAMGFromTemplate(cur.amTemplate, e)
		} else {
			next.acls = copyACLs(ctx, cur)
		}
		if cur.children == nil {
			cur.children = make(map[string]*node)
		}
		cur.children[e] = next
		cur = next
		cur.Lock()
	}
	// Only way out of the loop is via a return or exhausting all elements.  In
	// the latter case both cur and cur.parent are locked.
	return cur, nil, nil
}

// findNode finds a node in the table and optionally creates a path to it.
//
// If a node is found, on return it and its parent are locked.
func (mt *mountTable) findNode(ctx ipc.ServerContext, elems []string, create bool, tags []mounttable.Tag) (*node, error) {
	n, nelems, err := mt.traverse(ctx, elems, create)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, nil
	}
	if len(nelems) > 0 {
		n.parent.Unlock()
		n.Unlock()
		return nil, nil
	}
	if err := n.satisfies(mt, ctx, tags); err != nil {
		n.parent.Unlock()
		n.Unlock()
		return nil, err
	}
	return n, nil
}

// findMountPoint returns the first mount point encountered in the path and
// any elements remaining of the path.
//
// If a mountpoint is found, on return it and its parent are locked.
func (mt *mountTable) findMountPoint(ctx ipc.ServerContext, elems []string) (*node, []string, error) {
	n, nelems, err := mt.traverse(ctx, elems, false)
	if err != nil {
		return nil, nil, err
	}
	if n == nil {
		return nil, nil, nil
	}
	if !n.mount.isActive() {
		removed := n.removeUseless()
		n.parent.Unlock()
		n.Unlock()
		// If we removed the node, see if we can remove any of its
		// ascendants.
		if removed {
			mt.removeUselessRecursive(elems[:len(elems)-1])
		}
		return nil, nil, nil
	}
	return n, nelems, nil
}

// Authorize verifies that the client has access to the requested node.
// Since we do the check at the time of access, we always return OK here.
func (ms *mountContext) Authorize(ctx security.Context) error {
	return nil
}

// ResolveStep returns the next server in a resolution, the name remaining below that server,
// and whether or not that server is another mount table.
func (ms *mountContext) ResolveStepX(ctx ipc.ServerContext) (entry naming.VDLMountEntry, err error) {
	return ms.ResolveStep(ctx)
}

// ResolveStep returns the next server in a resolution in the form of a MountEntry.  The name
// in the mount entry is the name relative to the server's root.
func (ms *mountContext) ResolveStep(ctx ipc.ServerContext) (entry naming.VDLMountEntry, err error) {
	vlog.VI(2).Infof("ResolveStep %q", ms.name)
	mt := ms.mt
	// Find the next mount point for the name.
	n, elems, werr := mt.findMountPoint(ctx, ms.elems)
	if werr != nil {
		err = werr
		return
	}
	if n == nil {
		entry.Name = ms.name
		if len(ms.elems) == 0 {
			err = verror.New(naming.ErrNoSuchNameRoot, ctx.Context(), ms.name)
		} else {
			err = verror.New(naming.ErrNoSuchName, ctx.Context(), ms.name)
		}
		return
	}
	n.parent.Unlock()
	defer n.Unlock()
	entry.Servers = n.mount.servers.copyToSlice()
	entry.Name = strings.Join(elems, "/")
	entry.MT = n.mount.mt
	return
}

func hasMTFlag(flags naming.MountFlag) bool {
	return (flags & naming.MT) == naming.MT
}

func hasReplaceFlag(flags naming.MountFlag) bool {
	return (flags & naming.Replace) == naming.Replace
}

// Mount a server onto the name in the receiver.
func (ms *mountContext) Mount(ctx ipc.ServerContext, server string, ttlsecs uint32, flags naming.MountFlag) error {
	return ms.MountX(ctx, server, nil, ttlsecs, flags)
}

func (ms *mountContext) MountX(ctx ipc.ServerContext, server string, patterns []security.BlessingPattern, ttlsecs uint32, flags naming.MountFlag) error {
	if len(patterns) == 0 {
		// No patterns provided in the request, take the conservative
		// approach and assume that the server being mounted will
		// present the same blessings as the client calling Mount.
		blessings, _ := ctx.RemoteBlessings().ForContext(ctx)
		for _, b := range blessings {
			patterns = append(patterns, security.BlessingPattern(b))
		}
	}
	mt := ms.mt
	if ttlsecs == 0 {
		ttlsecs = 10 * 365 * 24 * 60 * 60 // a really long time
	}
	vlog.VI(2).Infof("*********************Mount %q -> %v%s", ms.name, patterns, server)

	// Make sure the server address is reasonable.
	epString := server
	if naming.Rooted(server) {
		epString, _ = naming.SplitAddressName(server)
	}
	_, err := veyron2.NewEndpoint(epString)
	if err != nil {
		return fmt.Errorf("malformed address %q for mounted server %q", epString, server)
	}

	// Find/create node in namespace and add the mount.
	n, werr := mt.findNode(ctx, ms.elems, true, mountTags)
	if werr != nil {
		return werr
	}
	if n == nil {
		return verror.New(naming.ErrNoSuchNameRoot, ctx.Context(), ms.name)
	}
	// We don't need the parent lock
	n.parent.Unlock()
	defer n.Unlock()
	if hasReplaceFlag(flags) {
		n.mount = nil
	}
	wantMT := hasMTFlag(flags)
	if n.mount == nil {
		n.mount = &mount{servers: newServerList(), mt: wantMT}
	} else {
		if wantMT != n.mount.mt {
			return fmt.Errorf("MT doesn't match")
		}
	}
	n.mount.servers.add(server, patterns, time.Duration(ttlsecs)*time.Second)
	return nil
}

// fullName is for debugging only and should not normally be called.
func (n *node) fullName() string {
	if n.parent == nil || n.parent.parent == nil {
		return ""
	}
	for k, c := range n.parent.children {
		if c == n {
			return n.parent.fullName() + "/" + k
		}
	}
	return n.parent.fullName() + "/" + "?"
}

// removeUseless removes a node and all of its ascendants that are not useful.
//
// We assume both n and n.parent are locked.
func (n *node) removeUseless() bool {
	if len(n.children) > 0 || n.mount.isActive() || n.explicitACLs {
		return false
	}
	for k, c := range n.parent.children {
		if c == n {
			delete(n.parent.children, k)
			break
		}
	}
	return true
}

// removeUselessRecursive removes any useless nodes on the tail of the path.
func (mt *mountTable) removeUselessRecursive(elems []string) {
	for i := len(elems); i > 0; i-- {
		n, nelems, _ := mt.traverse(nil, elems[:i-1], false)
		if n == nil {
			break
		}
		if nelems != nil {
			n.parent.Unlock()
			n.Unlock()
			break
		}
		removed := n.removeUseless()
		n.parent.Unlock()
		n.Unlock()
		if !removed {
			break
		}
	}
}

// Unmount removes servers from the name in the receiver. If server is specified, only that
// server is removed.
func (ms *mountContext) Unmount(ctx ipc.ServerContext, server string) error {
	vlog.VI(2).Infof("*********************Unmount %q, %s", ms.name, server)
	mt := ms.mt
	n, err := mt.findNode(ctx, ms.elems, false, mountTags)
	if err != nil {
		return err
	}
	if n == nil {
		return nil
	}
	if server == "" {
		n.mount = nil
	} else if n.mount != nil && n.mount.servers.remove(server) == 0 {
		n.mount = nil
	}
	removed := n.removeUseless()
	n.parent.Unlock()
	n.Unlock()
	if removed {
		// If we removed the node, see if we can also remove
		// any of its ascendants.
		mt.removeUselessRecursive(ms.elems[:len(ms.elems)-1])
	}
	return nil
}

// Delete removes the receiver.  If all is true, any subtree is also removed.
func (ms *mountContext) Delete(ctx ipc.ServerContext, deleteSubTree bool) error {
	vlog.VI(2).Infof("*********************Delete %q, %v", ms.name, deleteSubTree)
	if len(ms.elems) == 0 {
		// We can't delete the root.
		return fmt.Errorf("cannot delete root node")
	}
	mt := ms.mt
	// Find and lock the parent node.
	n, err := mt.findNode(ctx, ms.elems, false, removeTags)
	if err != nil {
		return err
	}
	if n == nil {
		return nil
	}
	defer n.parent.Unlock()
	defer n.Unlock()
	if !deleteSubTree && len(n.children) > 0 {
		return fmt.Errorf("cannot delete %s: has children", ms.name)
	}
	delete(n.parent.children, ms.elems[len(ms.elems)-1])
	return nil
}

// A struct holding a partial result of Glob.
type globEntry struct {
	n    *node
	name string
}

// globStep is called with n and n.parent locked.  Returns with both unlocked.
func (mt *mountTable) globStep(n *node, name string, pattern *glob.Glob, ctx ipc.ServerContext, ch chan<- naming.VDLMountEntry) {
	vlog.VI(2).Infof("globStep(%s, %s)", name, pattern)

	// If this is a mount point, we're done.
	if m := n.mount; m != nil {
		removed := n.removeUseless()
		if removed {
			n.parent.Unlock()
			n.Unlock()
			return
		}
		// Don't need the parent lock anymore.
		n.parent.Unlock()
		me := naming.VDLMountEntry{
			Name:    name,
			Servers: m.servers.copyToSlice(),
			MT:      n.mount.mt,
		}
		// Unlock while we are sending on the channel to avoid livelock.
		n.Unlock()
		ch <- me
		return
	}

	if !pattern.Finished() {
		// Recurse through the children.  OK if client has read access to the
		// directory or has traverse access to the directory and any access to the child.
		allAllowed := true
		if err := n.satisfies(mt, ctx, globTags); err != nil {
			allAllowed = false
			if err := n.satisfies(mt, ctx, traverseTags); err != nil {
				n.parent.Unlock()
				n.Unlock()
				return
			}
		}
		children := make(map[string]*node, len(n.children))
		for k, c := range n.children {
			children[k] = c
		}
		n.parent.Unlock()
		for k, c := range children {
			// At this point, n lock is held.
			if ok, _, suffix := pattern.MatchInitialSegment(k); ok {
				c.Lock()
				if !allAllowed {
					// If child allows any access show it.  Otherwise, skip.
					if err := c.satisfies(mt, ctx, allTags); err != nil {
						c.Unlock()
						continue
					}
				}
				mt.globStep(c, naming.Join(name, k), suffix, ctx, ch)
				n.Lock()
			}
		}
		// Relock the node and its parent in the correct order.
		// Safe to access n.parent when its unlocked because it never changes.
		n.Unlock()
		n.parent.Lock()
		n.Lock()
	}

	// Remove if no longer useful.
	if n.removeUseless() || pattern.Len() != 0 {
		n.parent.Unlock()
		n.Unlock()
		return
	}

	// To see anything, one has to have some access to the node.  Don't need the parent lock anymore.
	n.parent.Unlock()
	if err := n.satisfies(mt, ctx, allTags); err != nil {
		n.Unlock()
		return
	}
	// Unlock while we are sending on the channel to avoid livelock.
	n.Unlock()
	ch <- naming.VDLMountEntry{Name: name}
}

// Glob finds matches in the namespace.  If we reach a mount point before matching the
// whole pattern, return that mount point.
//
// pattern is a glob pattern as defined by the veyron/lib/glob package.
//
// To avoid livelocking an application, Glob grabs and releases locks as it descends the tree
// and holds no locks while writing to the channel.  As such a glob can interleave with other
// operations that add or remove nodes.  The result returned by glob may, therefore, represent
// a state that never existed in the mounttable.  For example, if someone removes c/d and later
// adds a/b while a Glob is in progress, the Glob may return a set of nodes that includes both
// c/d and a/b.
func (ms *mountContext) Glob__(ctx ipc.ServerContext, pattern string) (<-chan naming.VDLMountEntry, error) {
	vlog.VI(2).Infof("mt.Glob %v", ms.elems)

	g, err := glob.Parse(pattern)
	if err != nil {
		return nil, err
	}

	mt := ms.mt
	ch := make(chan naming.VDLMountEntry)
	go func() {
		defer close(ch)
		// If there was an access error, just ignore the entry, i.e., make it invisible.
		n, err := mt.findNode(ctx, ms.elems, false, nil)
		if err != nil {
			return
		}
		// If the current name is not fully resolvable on this nameserver we
		// don't need to evaluate the glob expression. Send a partially resolved
		// name back to the client.
		if n == nil {
			ms.linkToLeaf(ctx, ch)
			return
		}
		mt.globStep(n, "", g, ctx, ch)
	}()
	return ch, nil
}

func (ms *mountContext) linkToLeaf(ctx ipc.ServerContext, ch chan<- naming.VDLMountEntry) {
	n, elems, err := ms.mt.findMountPoint(ctx, ms.elems)
	if err != nil || n == nil {
		return
	}
	n.parent.Unlock()
	servers := n.mount.servers.copyToSlice()
	for i, s := range servers {
		servers[i].Server = naming.Join(s.Server, strings.Join(elems, "/"))
	}
	n.Unlock()
	ch <- naming.VDLMountEntry{Name: "", Servers: servers}
}

func (ms *mountContext) SetACL(ctx ipc.ServerContext, tam access.TaggedACLMap, etag string) error {
	vlog.VI(2).Infof("SetACL %q", ms.name)
	mt := ms.mt

	// Find/create node in namespace and add the mount.
	n, err := mt.findNode(ctx, ms.elems, true, setTags)
	if err != nil {
		return err
	}
	if n == nil {
		// TODO(p): can this even happen?
		return verror.New(naming.ErrNoSuchName, ctx.Context(), ms.name)
	}
	n.parent.Unlock()
	defer n.Unlock()
	n.acls, err = n.acls.Set(etag, tam)
	if err == nil {
		n.explicitACLs = true
	}
	return err
}

func (ms *mountContext) GetACL(ctx ipc.ServerContext) (access.TaggedACLMap, string, error) {
	vlog.VI(2).Infof("GetACL %q", ms.name)
	mt := ms.mt

	// Find node in namespace and add the mount.
	n, err := mt.findNode(ctx, ms.elems, false, getTags)
	if err != nil {
		return nil, "", err
	}
	if n == nil {
		return nil, "", verror.New(naming.ErrNoSuchName, ctx.Context(), ms.name)
	}
	n.parent.Unlock()
	defer n.Unlock()
	etag, tam := n.acls.Get()
	return tam, etag, nil
}
