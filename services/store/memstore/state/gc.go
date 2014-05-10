package state

// The store values include references based on pub.ID.  We use a garbage
// collection mechanism to remove values when they are no longer referenced.
// There are two general techniques for garbage collection: 1) infrequent
// tracing collection (like mark/sweep, etc.), and 2) reference counting.
// Reference counting is more responsive but it is harder to deal with cycles.
//
// We provide an "instant GC" semantics, meaning that once a value has become
// garbage, it is no longer returned as the result of any subsequent query.
//
// To satisfy the responsiveness requirement, we use a garbage collection scheme
// based on reference counting.  Cells are collected as soon as they become
// garbage.  This happens whenever the reference count becomes zero, but it can
// also happen when the refcount is nonzero, but the cell is part of a garbage
// cycle.
//
// The scheme here is based on the following.
//
//     Concurrent Cycle Collection in Reference Counted Systems
//     David F. Bacon and V.T. Rajan
//     IBM T.J. Watson Research Center
//     Proceedings of the European Conference on Object-Oriented Programming
//     June 2001
//     http://researcher.watson.ibm.com/researcher/files/us-bacon/Bacon01Concurrent.pdf
//
// This implementation is based on the "synchronous" algorithm, not the
// concurrent one.
//
// The central idea is that garbage collection is invoked when a refcount is
// decremented.  If the refcount becomes zero, then the cell is certainly
// garbage, and it can be reclaimed.  If the refcount is nonzero, then the cell
// might still be garbage.  The cell is added to a "gcRoots" table for later
// analysis.
//
// When GC is forced (due to a new query, for example), the graph reachable from
// the gcRoots is traversed to see if any of the cells are involved in garbage
// cycles.  The traversal decrements refcounts based on internal references
// (references reachable through gcRoots).  If any the the refcounts now become
// zero, that means all references are internal and the cell is actually
// garbage.
//
// That means the worst case GC cost is still linear in the number of elements
// in the store.  However, it is observed that in a large number of cases in
// practice, refcounts are either 0 or 1, so the gcRoots mechanism is not
// invoked.
//
// In our case, some directories will have multiple links, coming from
// replication group directories for example.  Whenver one of the replication
// group links is removed, we'll have to invoke the garbage collector to
// traverse the subgraph that was replicated.  This is similar to the cost that
// we would pay if the reachable subgraph became garbage, where we have to
// delete all of the cells in the subgraph.
//
// To improve performance, we delay the traversal, using the gcRoots set, until
// the GC is forced.
import (
	"veyron/services/store/memstore/refs"

	"veyron2/storage"
)

// color is the garbage collection color of a cell.
type color uint

const (
	black  color = iota // In use or free.
	gray                // Possible member of cycle.
	white               // Member of a garbage cycle.
	purple              // Possible root of cycle.
)

// ref increments the reference count.  The cell can't be garbage, so color it
// black.
func (sn *MutableSnapshot) ref(id storage.ID) {
	c := sn.deref(id)
	c.color = black
	c.refcount++
}

// unref decrements the reference count.  If the count reaches 0, the cell is
// garbage.  Otherwise the cell might be part of a cycle, so add it to the
// gcRoots.
func (sn *MutableSnapshot) unref(id storage.ID) {
	c := sn.deref(id)
	if c.refcount == 0 {
		panic("unref(): refcount is already zero")
	}
	c.refcount--
	if c.refcount == 0 {
		sn.release(c)
	} else {
		sn.possibleRoot(c)
	}
}

// release deletes a cell once the reference count has reached zero.  Delete the
// cell and all of its references.
func (sn *MutableSnapshot) release(c *Cell) {
	c.color = black
	c.refs.Iter(func(it interface{}) bool {
		sn.unref(it.(*refs.Ref).ID)
		return true
	})
	if !c.buffered {
		sn.delete(c)
	}
}

// possibleRoot marks the cell as the possible root of a cycle.  The cell is
// colored purple and added to the gcRoots.
func (sn *MutableSnapshot) possibleRoot(c *Cell) {
	if c.color != purple {
		c.color = purple
		if !c.buffered {
			c.buffered = true
			sn.gcRoots[c.ID] = struct{}{}
		}
	}
}

// GC is called to perform a garbage collection.
//
//   - markRoots traverses all of the cells reachable from the roots,
//     decrementing reference counts due to cycles.
//
//   - scanRoots traverses the cells reachable from the roots, coloring white if
//     the refcount is zero, or coloring black otherwise.  Note that a white
//     cell might be restored to black later if is reachable through some other
//     live path.
//
//   - collectRoots removes all remaining white nodes, which are cyclic garbage.
func (sn *MutableSnapshot) gc() {
	sn.markRoots()
	sn.scanRoots()
	sn.collectRoots()
}

// markRoots traverses the cells reachable from the roots, marking them gray.
// If another root is encountered during a traversal, it is removed from the
// gcRoots.
func (sn *MutableSnapshot) markRoots() {
	for id, _ := range sn.gcRoots {
		c := sn.deref(id)
		if c.color == purple {
			sn.markGray(c)
		} else {
			c.buffered = false
			delete(sn.gcRoots, id)
			if c.color == black && c.refcount == 0 {
				sn.delete(c)
			}
		}
	}
}

// markGray colors a cell gray, the decrements the refcounts of the children,
// and marks them gray recursively.  The result is that counts for "internal"
// references (references reachable from the roots) are removed.  Then, if the
// reference count of a root reaches zero, it must be reachable only from
// internal references, so it is part of a garbage cycle, and it can be deleted.
func (sn *MutableSnapshot) markGray(c *Cell) {
	if c.color == gray {
		return
	}
	c.color = gray
	c.refs.Iter(func(it interface{}) bool {
		id := it.(*refs.Ref).ID
		nc := sn.deref(id)
		nc.refcount--
		sn.markGray(nc)
		return true
	})
}

// scanRoots traverses the graph from gcRoots.  If a cell has a non-zero
// refcount, that means it is reachable from an external reference, so it is
// live.  In that case, the cell and reachable children are live.  Otherwise,
// the refcount is zero, and the cell is colored white to indicate that it may
// be garbage.
func (sn *MutableSnapshot) scanRoots() {
	for id, _ := range sn.gcRoots {
		sn.scan(sn.deref(id))
	}
}

// scan gray nodes, coloring them black if the refcount is nonzero, or white
// otherwise.
func (sn *MutableSnapshot) scan(c *Cell) {
	if c.color != gray {
		return
	}
	if c.refcount > 0 {
		sn.scanBlack(c)
	} else {
		c.color = white
		c.refs.Iter(func(it interface{}) bool {
			id := it.(*refs.Ref).ID
			sn.scan(sn.deref(id))
			return true
		})
	}
}

// scanBlack colors a cell and its subgraph black, restoring refcounts.
func (sn *MutableSnapshot) scanBlack(c *Cell) {
	c.color = black
	c.refs.Iter(func(it interface{}) bool {
		id := it.(*refs.Ref).ID
		nc := sn.deref(id)
		nc.refcount++
		if nc.color != black {
			sn.scanBlack(nc)
		}
		return true
	})
}

// collectRoots frees all the cells that are colored white.
func (sn *MutableSnapshot) collectRoots() {
	for id, _ := range sn.gcRoots {
		c := sn.deref(id)
		c.buffered = false
		sn.collectWhite(c)
	}
	sn.gcRoots = make(map[storage.ID]struct{})
}

// collectWhite frees cells that are colored white.
func (sn *MutableSnapshot) collectWhite(c *Cell) {
	if c.color != white || c.buffered {
		return
	}
	c.color = black
	c.refs.Iter(func(it interface{}) bool {
		id := it.(*refs.Ref).ID
		sn.collectWhite(sn.deref(id))
		return true
	})
	sn.delete(c)
}
