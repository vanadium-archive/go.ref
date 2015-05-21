// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

import (
	"sync"
)

// resourceNode is a node in a dependency graph. This graph is used to ensure
// that when a resource is freed, downstream resources are also freed. For
// example, closing a store closes all downstream transactions, snapshots and
// streams.
type resourceNode struct {
	mu       sync.Mutex
	parent   *resourceNode
	children map[*resourceNode]func()
}

func newResourceNode() *resourceNode {
	return &resourceNode{
		children: make(map[*resourceNode]func()),
	}
}

// addChild adds a parent-child relation between this node and the provided
// node. The provided function is called to close the child when this node is
// closed.
func (r *resourceNode) addChild(node *resourceNode, closefn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.children == nil {
		panic("already closed")
	}
	node.parent = r
	r.children[node] = closefn
}

// removeChild removes the parent-child relation between this node and the
// provided node, enabling Go's garbage collector to free the resources
// associated with the node if there are no more references to it.
func (r *resourceNode) removeChild(node *resourceNode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.children == nil {
		// Already closed.
		return
	}
	delete(r.children, node)
}

// close closes this node and detaches it from its parent. All child nodes
// are closed using close functions provided to addChild.
func (r *resourceNode) close() {
	r.mu.Lock()
	if r.parent != nil {
		// If there is a node V with parent P and we decide to explicitly close V,
		// then we need to remove V from P's children list so that we don't close
		// V again when P is closed.
		r.parent.removeChild(r)
		r.parent = nil
	}
	// Copy the children map to a local variable so that the removeChild step
	// executed from children won't affect the map while we iterate through it.
	children := r.children
	r.children = nil
	r.mu.Unlock()
	for _, closefn := range children {
		closefn()
	}
}
