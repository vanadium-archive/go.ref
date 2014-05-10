// Package rb implements functional sets using red-black trees.  This
// implementation is based on Chris Okasaki's functional red-black trees.
//
//    Article: Functional Pearls
//    Title: Red-Black Trees in a Functional Setting
//    Author: Chris Okasaki
//    J. Functional Programming, Jan 1993
//    http://www.cs.tufts.edu/~nr/cs257/archive/chris-okasaki/redblack99.ps‎
//
// Okasaki doesn't define deletion.  This version of deletion is based on Stefan
// Khar's untyped implementation in Haskell.
//
//   Title: Red-black trees with types.
//   Author: Stefan Khars
//   J. Functional Programming, July 2001
//   http://www.cs.kent.ac.uk/people/staff/smk/redblack/Untyped.hs
//
// By "functional," we mean that functions behave like mathematical functions:
// given the same argument, the function always returns the same value.  The
// practical consequence is that a set is immutable and persistent.  It can't be
// changed once it is constructed.  The operations that mutate the set (Put and
// Remove) return a new set instead.
//
// Red-black trees have the following invariants.
//    - Every node is colored either red or black.
//    - The root is black.
//    - RED: Red nodes have only black children.
//    - BLACK: All paths from the root to the leaves have the same number of black nodes.
//
// These invariants imply that the longest path to a leaf is at most twice as
// long as the shortest path, hence the tree is balanced.  Insertion and deletion
// take O(log N) time, where N is the number of elements in the set.
package rb

import (
	"veyron/runtimes/google/lib/functional"
)

// Node colors are RED and BLACK
type color int

const (
	BLACK color = iota
	RED
)

// rbSet is the red-black set.
type rbSet struct {
	root *node
	cmp  functional.Comparator
	size int
}

// The tree is constructed from Node.  Each Node has a color,
// two children, and a label.
type node struct {
	color color
	key   interface{}
	left  *node
	right *node
}

// iterator contains a path to a node.
type iterator struct {
	path []*node
}

func newNode(color color, key interface{}, left, right *node) *node {
	return &node{color: color, key: key, left: left, right: right}
}

func isBlack(n *node) bool {
	return n == nil || n.color == BLACK
}

func isRed(n *node) bool {
	return n != nil && n.color == RED
}

func makeBlack(n *node) *node {
	if isBlack(n) {
		return n
	}
	return newNode(BLACK, n.key, n.left, n.right)
}

func makeRed(n *node) *node {
	if isRed(n) {
		panic("Node is already RED")
	}
	return newNode(RED, n.key, n.left, n.right)
}

// NewSet returns an empty set using the comparator.
func NewSet(cmp functional.Comparator) functional.Set {
	return &rbSet{cmp: cmp}
}

func (s *rbSet) newSet(root *node, size int) *rbSet {
	return &rbSet{root: root, cmp: s.cmp, size: size}
}

func (s *rbSet) newSetWithCmp(root *node, cmp functional.Comparator, size int) *rbSet {
	return &rbSet{root: root, cmp: cmp, size: size}
}

// IsEmpty returns true iff the set is empty.
func (s *rbSet) IsEmpty() bool {
	return s.root == nil
}

// Len returns the size of the tree.
func (s *rbSet) Len() int {
	return s.size
}

// Contains returns true iff the element is in the set.
func (s *rbSet) Contains(key interface{}) bool {
	for n := s.root; n != nil; {
		if s.cmp(key, n.key) {
			n = n.left
		} else if s.cmp(n.key, key) {
			n = n.right
		} else {
			return true
		}
	}
	return false
}

// Get returns the element in the s.
func (s *rbSet) Get(key interface{}) (interface{}, bool) {
	for n := s.root; n != nil; {
		if s.cmp(key, n.key) {
			n = n.left
		} else if s.cmp(n.key, key) {
			n = n.right
		} else {
			return n.key, true
		}
	}
	return nil, false
}

// Put adds an element to the set, replacing any existing element.
func (s *rbSet) Put(key interface{}) functional.Set {
	root, sizeChanged := insert(s.cmp, key, s.root)
	size := s.size
	if sizeChanged {
		size++
	}
	return s.newSet(makeBlack(root), size)
}

// insert performs the actual insertion, returning the new node, and a bool
// indicating whether the tree size has changed.
//
// Tree balancing is performed bottom-up; the stratgey is for black grandparents
// to fix red-parent + red-child violations by rotating the tree and recoloring.
func insert(cmp functional.Comparator, key interface{}, n *node) (*node, bool) {
	switch {
	case n == nil:
		n = newNode(RED, key, nil, nil)
		return n, true
	case n.color == BLACK:
		switch {
		case cmp(key, n.key):
			left, sizeChanged := insert(cmp, key, n.left)
			if sizeChanged {
				n = balance(n.key, left, n.right)
			} else {
				n = newNode(BLACK, n.key, left, n.right)
			}
			return n, sizeChanged
		case cmp(n.key, key):
			right, sizeChanged := insert(cmp, key, n.right)
			if sizeChanged {
				n = balance(n.key, n.left, right)
			} else {
				n = newNode(BLACK, n.key, n.left, right)
			}
			return n, sizeChanged
		default:
			// key and n.key are in the same equivalence class according to cmp,
			// but we choose key as the representative.
			n = newNode(BLACK, key, n.left, n.right)
			return n, false
		}
	default: // n.color == RED
		switch {
		case cmp(key, n.key):
			left, sizeChanged := insert(cmp, key, n.left)
			n = newNode(RED, n.key, left, n.right)
			return n, sizeChanged
		case cmp(n.key, key):
			right, sizeChanged := insert(cmp, key, n.right)
			n = newNode(RED, n.key, n.left, right)
			return n, sizeChanged
		default:
			// key and n.key are in the same equivalence class according to cmp,
			// but we choose the new key as the representative.
			n = newNode(RED, key, n.left, n.right)
			return n, false
		}
	}
}

// Operations on the tree that insert and delete nodes may violate the
// RED invariant.  The purpose of the balance function is to restore
// the RED invariant by rotating the tree whenever one of the
// arguments has a RED root with a RED child.  In addition, if both
// arguments have a RED root, we migrate the RED to the root, and make
// both subtrees BLACK.
func balance(key interface{}, left, right *node) *node {
	if isRed(left) {
		switch {
		case isRed(right):
			// This is an optimization not in Okasaki's original algorithm, to
			// reduce the number of reds by migrating them upward.
			return newNode(RED, key,
				newNode(BLACK, left.key, left.left, left.right),
				newNode(BLACK, right.key, right.left, right.right))
		case isRed(left.left):
			return newNode(RED, left.key,
				newNode(BLACK, left.left.key, left.left.left, left.left.right),
				newNode(BLACK, key, left.right, right))
		case isRed(left.right):
			return newNode(RED, left.right.key,
				newNode(BLACK, left.key, left.left, left.right.left),
				newNode(BLACK, key, left.right.right, right))
		}
	}
	if isRed(right) {
		switch {
		case isRed(right.left):
			return newNode(RED, right.left.key,
				newNode(BLACK, key, left, right.left.left),
				newNode(BLACK, right.key, right.left.right, right.right))
		case isRed(right.right):
			return newNode(RED, right.key,
				newNode(BLACK, key, left, right.left),
				newNode(BLACK, right.right.key, right.right.left, right.right.right))
		}
	}
	return newNode(BLACK, key, left, right)
}

// Removes removes an element from the set.
func (s *rbSet) Remove(key interface{}) functional.Set {
	root := remove(s.cmp, key, s.root)
	if root == s.root {
		return s // Nothing changed.
	}
	return s.newSet(makeBlack(root), s.size-1)
}

func remove(cmp functional.Comparator, key interface{}, n *node) *node {
	switch {
	case n == nil:
		return nil
	case cmp(key, n.key):
		left := remove(cmp, key, n.left)
		switch {
		case left == n.left:
			return n // Nothing changed.
		case isBlack(n.left):
			return balanceLeft(n.key, left, n.right)
		default:
			return newNode(RED, n.key, left, n.right)
		}
	case cmp(n.key, key):
		right := remove(cmp, key, n.right)
		switch {
		case right == n.right:
			return n // Nothing changed.
		case isBlack(n.right):
			return balanceRight(n.key, n.left, right)
		default:
			return newNode(RED, n.key, n.left, right)
		}
	default:
		// If either of n.left or n.right is RED, the result of concat() might
		// violate the RED invariant at the root.  This can happen only if
		// n.color == BLACK.  If so, the recursive calls to remove in this
		// function are followed by calls to balanceLeft or balanceRight to
		// rebalance the tree.  See the balanceLeft comment below.
		return concat(n.left, n.right)
	}
}

// balanceLeft is called to balance a node after a deletion in the left branch.
// The original node had key "key", "left" is the new left subtree after the
// deletion was performed, and "right" is the original right child.
//
// REQUIRES: Before deletion, the left node was BLACK.  Since we have deleted an
// element from the left subtree, the black depth of "left" is now one less than
// the black depth of "right".
//
// ALLOWED: The left subtree may violate the RED invariant, but only at the
// root (left might be RED and also have a RED child, but otherwise it is
// balanced).
//
// To preserve the BLACK invariant, we need to subtract a BLACK from the right
// subtree, or else rotate the tree to preserve the invariant.  In all cases, if
// the original root node was BLACK, then the resulting tree has one less black
// depth; if the original tree was RED, the resulting tree has the same black
// depth.
//
// There are three cases.
//
//   1. If the new left child is RED, change the left child to BLACK and create
//      a RED root.  If the original root node was BLACK, the total black depth
//      is decreased by one.
//
//   2. Otherwise, the left child is BLACK.  If the right child is also BLACK,
//      make the right child RED.  This may invalidate the RED invariant, so
//      call the Balance() method to fix it.
//
//   3. Otherwise, left is BLACK, right is RED, and the original root was BLACK.
//      Since the left tree is BLACK, the right node *must* have two BLACK
//      non-leaf children.  Pick the left one, and rotate it into the left
//      subtree.
func balanceLeft(key interface{}, left, right *node) *node {
	switch {
	case isRed(left):
		return newNode(RED, key, newNode(BLACK, left.key, left.left, left.right), right)
	case isBlack(right):
		return balance(key, left, makeRed(right))
	default: // isRed(right) && isBlack(right.left)
		return newNode(RED, right.left.key,
			newNode(BLACK, key, left, right.left.left),
			balance(right.key, right.left.right, makeRed(right.right)))
	}
}

func balanceRight(key interface{}, left, right *node) *node {
	switch {
	case isRed(right):
		return newNode(RED, key, left, newNode(BLACK, right.key, right.left, right.right))
	case isBlack(left):
		return balance(key, makeRed(left), right)
	default: // isRed(left) && isBlack(left.right)
		return newNode(RED, left.right.key,
			balance(left.key, makeRed(left.left), left.right.left),
			newNode(BLACK, key, left.right.right, right))
	}
}

// Append the left and right trees.  The max of the left is smaller than the min
// of the right, and the two trees have the same black depth.
//
// The BLACK invariant holds for the resulting tree, and it has the same black
// depth as the input trees.
//
// If both arguments have BLACK roots, the RED invariant will hold for the
// resulting tree.  However, if either argument has a RED root, the RED
// invariant may not hold for the result, which may be violated at the root
// (only).  In this case, the caller will need to rebalance the tree.  See the
// comments for the recursive calls below.
func concat(left, right *node) *node {
	switch {
	case left == nil:
		return right
	case right == nil:
		return left
	case left.color == BLACK && right.color == BLACK:
		// middle has the same black depth as left.right and right.left.
		middle := concat(left.right, right.left)
		if isRed(middle) {
			// middle may have a RED violation at the root, but if it does, the
			// subtrees are balanced, so we can balance the result by giving
			// them BLACK parents.
			return newNode(RED, middle.key,
				newNode(BLACK, left.key, left.left, middle.left),
				newNode(BLACK, right.key, middle.right, right.right))
		} else {
			// left.left, middle, and right.right all have the same black depth,
			// middle is BLACK, and we're adding a new BLACK node.  Use
			// balanceLeft to restore the BLACK invariant.
			return balanceLeft(left.key, left.left,
				newNode(BLACK, right.key, middle, right.right))
		}
	case left.color == RED && right.color == RED:
		// middle has the same black depth as left.right and right.left.
		middle := concat(left.right, right.left)
		if isRed(middle) {
			// All parts, left.left, middle.left, middle.right, and right.right,
			// have the same black depth, and they are all BLACK.  To preserve
			// the black depth, use only RED modes, violating the RED invariant.
			return newNode(RED, middle.key,
				newNode(RED, left.key, left.left, middle.left),
				newNode(RED, right.key, middle.right, right.right))
		} else {
			// left.left, middle, and right.right all have the same black depth
			// and all are BLACK.  To preserve the black depth, use only RED
			// modes, violating the RED invariant.
			return newNode(RED, left.key, left.left,
				newNode(RED, right.key, middle, right.right))
		}
	case left.color == RED: // right.color == BLACK
		// left.left, left.right, and right have the same black depth.  To preserve
		// the black depth, return a RED node, possibly violating the RED invariant.
		return newNode(RED, left.key, left.left, concat(left.right, right))
	default: // left.color == BLACK && right.color == RED
		// left, right.left, and right.right have the same black depth.  To
		// preserve the black depth, return a RED node, possibly violating the
		// RED invariant.
		return newNode(RED, right.key, concat(left, right.left), right.right)
	}
}

// Iter applies a function to each element of the set in order.  The iteration
// terminates if the function returns false.
func (s *rbSet) Iter(f func(it interface{}) bool) {
	if s.root != nil {
		s.root.iter(f)
	}
}

func (n *node) iter(f func(it interface{}) bool) {
	if n.left != nil {
		n.left.iter(f)
	}
	f(n.key)
	if n.right != nil {
		n.right.iter(f)
	}
}

// Map applies a function to each element of the set in order, replacing the
// elements value.  The ordering of the elements must be preserved.
//
//    { e1, e2, ..., eN }.Map(f) = { f(e1), f(e2), ..., f(eN) }
//
// Typically, this is use in dictionaries to update the value part of a
// key/value pair, keeping the key unchanged, so preserving the order.
func (s *rbSet) Map(f func(it interface{}) interface{}, cmp functional.Comparator) functional.Set {
	if s.root == nil {
		return s
	}
	return s.newSetWithCmp(s.root.fmap(f), cmp, s.size)
}

func (n *node) fmap(f func(it interface{}) interface{}) *node {
	left := n.left
	if left != nil {
		left = left.fmap(f)
	}
	key := f(n.key)
	right := n.right
	if right != nil {
		right = right.fmap(f)
	}
	return newNode(n.color, key, left, right)
}

// Fold applies a function to the elements of the set in order, accumulating the
// results.
//
//     { e1, e2, ..., eN }.Fold(f, x) = f(eN, ... f(e2, f(e1, x)))
//
// For example, a sum could be computed as follows.
//
//     s.Fold(func (it, accum interface{}) interface{} {
//        return it.(int) + accum.(int)
//     })
//
func (s *rbSet) Fold(f func(it, x interface{}) interface{}, x interface{}) interface{} {
	if s.root == nil {
		return x
	}
	return s.root.fold(f, x)
}

func (n *node) fold(f func(it, x interface{}) interface{}, x interface{}) interface{} {
	if n.left != nil {
		x = n.left.fold(f, x)
	}
	x = f(n.key, x)
	if n.right != nil {
		x = n.right.fold(f, x)
	}
	return x
}

// iteration.
func (s *rbSet) Iterator() functional.Iterator {
	if s.root == nil {
		return &iterator{}
	}
	var path []*node
	for node := s.root; node != nil; node = node.left {
		path = append(path, node)
	}
	return &iterator{path: path}
}

// IsValid returns true iff the iterator refers to an element.
func (it *iterator) IsValid() bool {
	return len(it.path) != 0
}

// Get returns the current element.
func (it *iterator) Get() interface{} {
	i := len(it.path)
	if i == 0 {
		return nil
	}
	return it.path[i-1].key
}

// Next advances to the next element.
func (it *iterator) Next() {
	path := it.path
	i := len(path)
	if i == 0 {
		return
	}
	current := path[i-1]

	// If there is a right child, descend to leftmost leaf.
	if current.right != nil {
		for node := current.right; node != nil; node = node.left {
			path = append(path, node)
		}
		it.path = path
		return
	}

	// If there is no right child, ascend to the nearest ancestor for which the
	// path branches left.  That's the next in-order element.
	for j := i - 2; j >= 0; j-- {
		parent := path[j]
		if current == parent.left {
			// Found a left branch.
			it.path = path[:j+1]
			return
		}
		current = parent
	}

	// This was the rightmost path; we're done.
	it.path = nil
}
