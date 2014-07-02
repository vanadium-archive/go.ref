package ipc

import (
	"strings"
	"sync"

	"veyron2/ipc"
	"veyron2/verror"
)

var (
	errNilDispatcher             = verror.BadArgf("ipc: can't register nil dispatcher")
	errRegisterOnStoppedDisptrie = verror.Abortedf("ipc: Register called on stopped dispatch trie")
)

// disptrie represents the dispatch trie, where each trie node holds a slice of
// dispatchers for that node, and a mapping of children keyed by slash-delimited
// object name fragment.  The trie performs longest-prefix name lookup, and is
// safe for concurrent access.
type disptrie struct {
	sync.RWMutex
	root *trienode
}

func newDisptrie() *disptrie {
	return &disptrie{root: new(trienode)}
}

// splitName splits the object name into its slash-separated components.
func splitName(name string) []string {
	name = strings.TrimLeft(name, "/")
	if name == "" {
		return nil
	}
	return strings.Split(name, "/")
}

// Register disp in the trie node represented by prefix.  Multiple dispatchers
// may be registered in the same node, and are stored in order of registration.
func (d *disptrie) Register(prefix string, disp ipc.Dispatcher) error {
	if disp == nil {
		return errNilDispatcher
	}
	name := splitName(prefix)
	var err error
	d.Lock()
	if d.root != nil {
		d.root.insert(name, disp)
	} else {
		err = errRegisterOnStoppedDisptrie
	}
	d.Unlock()
	return err
}

// Lookup returns the dispatchers registered under the trie node representing
// the longest match against the prefix.  Also returns the remaining unmatched
// suffix.
func (d *disptrie) Lookup(prefix string) (disps []ipc.Dispatcher, suffix string) {
	name := splitName(prefix)
	d.RLock()
	if d.root != nil {
		disps, suffix = d.root.lookup(name, nil)
	}
	d.RUnlock()
	return
}

// Stop dispatching on all registered dispatchers.  After this call it's
// guaranteed that no new lookups will be issued on the previously registered
// dispatchers.
func (d *disptrie) Stop() {
	// Unlink the root, which ensures all dispatchers are unregistered
	// atomically, and will not be consulted for future lookups.
	d.Lock()
	d.root = nil
	d.Unlock()
}

// trienode represents a single node in the dispatch trie.
type trienode struct {
	disps    []ipc.Dispatcher
	children map[string]*trienode
}

func (t *trienode) insert(name []string, disp ipc.Dispatcher) {
	if len(name) == 0 {
		t.disps = append(t.disps, disp)
		return
	}
	var child *trienode
	if t.children == nil {
		// No children, create new children and add new child node.
		child = new(trienode)
		t.children = map[string]*trienode{name[0]: child}
	} else if c, ok := t.children[name[0]]; ok {
		// Existing child, set child node.
		child = c
	} else {
		// Non-existing child, add new child node.
		child = new(trienode)
		t.children[name[0]] = child
	}
	child.insert(name[1:], disp)
}

func (t *trienode) lookup(name []string, prev []ipc.Dispatcher) ([]ipc.Dispatcher, string) {
	if len(t.disps) > 0 {
		prev = t.disps
	}
	if len(name) > 0 && t.children != nil {
		if child, ok := t.children[name[0]]; ok {
			return child.lookup(name[1:], prev)
		}
	}
	ret := strings.Join(name, "/")
	if strings.HasPrefix(ret, "/") && !strings.HasPrefix(ret, "//") {
		ret = "/" + ret
	}
	return prev, ret
}

func (t *trienode) walk(f func(ipc.Dispatcher)) {
	for _, disp := range t.disps {
		f(disp)
	}
	for _, child := range t.children {
		child.walk(f)
	}
}
