package state

import (
	"fmt"

	"veyron/services/store/memstore/acl"
	"veyron/services/store/memstore/pathregex"
	"veyron/services/store/memstore/refs"
	"veyron2/security"
	"veyron2/storage"
)

// Path matching is used to test whether a value has a pathname that matches a
// regular expression.  The snapshot is a labeled directed graph that can be
// viewed as a finite automaton, and the pathregex is a finite automaton, so the
// matching problem asks whether the intersection of the regular languages
// defined by these automata is nonempty.
//
// We implement it in two phases.
//
// The first phase is an optimization, where we compute the intersection
// automaton from the reversed automata.  This is typically more efficient
// because the subgraph formed from the incoming edges is usually much smaller
// than the entire graph.  Note that the snapshot is deterministic when viewed
// as a forward automaton, since every field of a value has a unique field name,
// but the reversed automaton is nondeterministic.  However, that doesn't have
// any bearing on the algorithm here.
//
// We keep a queue of work to do, and a visited set.  The queue contains a set
// of cells that are scheduled to be visited, along with the StateSet of the
// pathregex when visiting.  The visited table contains the set of cells that
// have already been visited, along with the StateSet when they were last
// visited.  We build a reducedGraph that contains only the edges in the
// intersection.
//
// On each step, the main search loop pulls an element from the queue.  If the
// cell has already been visited with a StateSet that is just as large as the
// one in the queue, quit.  Otherwise, compute the transitions for all the
// inRefs and add the new entries to the queue.  The search teriminates when the
// root is reached in a final state, or else the queue becomes empty and there
// is no match.
//
// Security properties are inherited, meaning that the properties are propagated
// down from the root along paths.  That means access control can't be performed
// during the first phase.
//
// During the second phase, we recompute the intersection from the forward
// automata, this time taking security into account.

// PathRegex is the result of compiling the path regular expression to forward
// and reverse NFAs.
type PathRegex struct {
	forwardSS forwardStateSet
	reverseSS reverseStateSet
}

// forwardStateSet and reverseStateSet wrap the pathregex.StateSet to avoid
// confusion about which automata they belong to.
type forwardStateSet struct {
	pathregex.StateSet
}

type reverseStateSet struct {
	pathregex.StateSet
}

// reducer is used to compute the reducedGraph.
type reducer struct {
	snapshot *snapshot
}

// reducedGraph is the forward reference graph, pruned to include only the
// references in the intersection automaton.  Does not prune edges inaccessible
// due to security restrictions.
type reducedGraph map[storage.ID]*reducedCell

type reducedCell struct {
	tags storage.TagList
	refs refs.Set
}

type reduceVisitedSet map[storage.ID]reverseStateSet
type reduceQueue map[storage.ID]reverseStateSet

// matcher is used to perform a forward match on the reducedGraph.
type matcher struct {
	graph    reducedGraph
	targetID storage.ID
}

// matchVisitedSet contains the checker/state-set encountered when visiting a
// node.  Since an entry in the store might have multiple name, it might be
// visited multiple times with different checkers.  The matchVisitedSet contains
// a list of checkers and state sets visited with each checker.
type matchVisitedSet map[storage.ID]*matchVisitedEntry

type matchVisitedEntry struct {
	checkers []*matchVisitedCheckerSet
}

type matchVisitedCheckerSet struct {
	checker *acl.Checker
	ss      forwardStateSet
}

// matchQueue contains the worklist for the matcher.
type matchQueue struct {
	queue []*matchQueueEntry
}

type matchQueueEntry struct {
	id      storage.ID
	checker *acl.Checker
	ss      forwardStateSet
}

// CompilePathRegex compiles the regular expression to a PathRegex.
func CompilePathRegex(regex string) (*PathRegex, error) {
	forwardSS, err := pathregex.Compile(regex)
	if err != nil {
		return nil, err
	}
	reverseSS, err := pathregex.CompileReverse(regex)
	if err != nil {
		return nil, err
	}
	p := &PathRegex{
		forwardSS: forwardStateSet{StateSet: forwardSS},
		reverseSS: reverseStateSet{StateSet: reverseSS},
	}
	return p, nil
}

// PathMatch returns true iff there is a name for the store value that matches
// the pathRegex.
func (sn *snapshot) PathMatch(pid security.PublicID, id storage.ID, regex *PathRegex) bool {
	r := &reducer{snapshot: sn}
	g := r.reduce(id, regex.reverseSS)
	m := &matcher{graph: g, targetID: id}
	checker := sn.newPermChecker(pid)
	return m.match(checker, sn.rootID, regex.forwardSS)
}

// reduce computes the reducedGraph (the intersection) without regard to
// security.
func (r *reducer) reduce(id storage.ID, rss reverseStateSet) reducedGraph {
	visited := reduceVisitedSet{}
	graph := reducedGraph{}
	graph.add(id)
	queue := reduceQueue{}
	queue.add(id, rss)
	for len(queue) != 0 {
		for id, ssUpdate := range queue {
			// Take an element from the queue.
			delete(queue, id)

			// Check whether it has already been visited.
			ss, changed := visited.add(id, ssUpdate)
			if !changed {
				// We already visited this node.
				continue
			}

			// Enqueue new entries for each of the inRefs.
			c := r.find(id)
			c.inRefs.Iter(func(it interface{}) bool {
				ref := it.(*refs.Ref)
				if ssNew := ss.step(ref.Path); !ssNew.IsReject() {
					graph.addRef(ref, id)
					queue.add(ref.ID, ssNew)
				}
				return true
			})
		}
	}
	graph.setTags(r.snapshot)
	return graph
}

// find returns the cell for the id.  Panics if the value doesn't exist.
func (r *reducer) find(id storage.ID) *Cell {
	c := r.snapshot.Find(id)
	if c == nil {
		panic(fmt.Sprintf("dangling reference: %s", id))
	}
	return c
}

// add updates the visited state to include ssUpdate.  Returns true iff the
// state set changed.
func (visited reduceVisitedSet) add(id storage.ID, ssUpdate reverseStateSet) (reverseStateSet, bool) {
	ss, ok := visited[id]
	if !ok {
		visited[id] = ssUpdate
		return ssUpdate, true
	}

	ssNew := reverseStateSet{StateSet: ss.Union(ssUpdate.StateSet)}
	if ssNew.Equals(ss.StateSet) {
		// Nothing changed.
		return ss, false
	}

	visited[id] = ssNew
	return ssNew, true
}

// add adds a new entry to the queue.
func (queue reduceQueue) add(id storage.ID, ss reverseStateSet) {
	if ssCurrent, ok := queue[id]; ok {
		ss = reverseStateSet{StateSet: ss.Union(ssCurrent.StateSet)}
	}
	queue[id] = ss
}

// add adds a reference if it doesn't already exist.
func (g reducedGraph) add(id storage.ID) *reducedCell {
	v, ok := g[id]
	if !ok {
		// Ignore the tags, they will be filled in later in setTags.
		v = &reducedCell{refs: refs.Empty}
		g[id] = v
	}
	return v
}

// addRef adds a forward edge (r.ID -> id) to the reduced graph.
func (g reducedGraph) addRef(ref *refs.Ref, id storage.ID) {
	v := g.add(ref.ID)
	v.refs = v.refs.Put(&refs.Ref{ID: id, Path: ref.Path, Label: ref.Label})
}

// setTags sets the tags values in the graph.
func (g reducedGraph) setTags(sn Snapshot) {
	for id, v := range g {
		c := sn.Find(id)
		if c == nil {
			panic(fmt.Sprintf("dangling reference: %s", id))
		}
		v.tags = c.Tags
	}
}

// match performs a secure forward match.
func (m *matcher) match(checker *acl.Checker, rootID storage.ID, rss forwardStateSet) bool {
	if !m.contains(rootID) {
		return false
	}
	visited := matchVisitedSet{}
	queue := matchQueue{}
	queue.add(rootID, checker, rss)
	for !queue.isEmpty() {
		// Take an entry from the queue.
		qentry := queue.pop()

		// Check whether it has already been visited.
		ss, changed := visited.add(qentry)
		if !changed {
			// We already visited this node.
			continue
		}

		// If we reached the target, we're done.
		if qentry.id == m.targetID && ss.IsFinal() {
			return true
		}

		// Enqueue new entries for each of the refs.
		src := m.find(qentry.id)
		src.refs.Iter(func(it interface{}) bool {
			ref := it.(*refs.Ref)
			if qentry.checker.IsAllowed(ref.Label) {
				if ssNew := ss.step(ref.Path); !ssNew.IsReject() {
					dst := m.find(ref.ID)
					checker := qentry.checker.Copy()
					checker.Update(dst.tags)
					if checker.IsAllowed(security.ReadLabel) {
						queue.add(ref.ID, checker, ssNew)
					}
				}
			}
			return true
		})
	}
	return false
}

// contains returns true iff the reduced graph contains the ID.
func (m *matcher) contains(id storage.ID) bool {
	_, ok := m.graph[id]
	return ok
}

// find returns the entry in the matchQueue.  Panics if the entry doesn't exist.
func (m *matcher) find(id storage.ID) *reducedCell {
	v, ok := m.graph[id]
	if !ok {
		panic(fmt.Sprintf("dangling reference: %s", id))
	}
	return v
}

// add updates the visited state to include ssUpdate.  Returns true iff the
// state set changed.
func (visited matchVisitedSet) add(qentry *matchQueueEntry) (forwardStateSet, bool) {
	e, ok := visited[qentry.id]
	if !ok {
		c := &matchVisitedCheckerSet{checker: qentry.checker, ss: qentry.ss}
		e = &matchVisitedEntry{checkers: []*matchVisitedCheckerSet{c}}
		visited[qentry.id] = e
		return c.ss, true
	}

	// Add the checker if it is different from the others.
	c := e.addCheckerSet(qentry.checker)

	// Update the state set.
	ssNew := c.ss.Union(qentry.ss.StateSet)
	if !ssNew.Equals(c.ss.StateSet) {
		c.ss = forwardStateSet{StateSet: ssNew}
		return c.ss, true
	}
	return c.ss, false
}

// addChecker add the checker if it is different from the others.  Returns true iff
// the set of checkers changed.
func (e *matchVisitedEntry) addCheckerSet(checker *acl.Checker) *matchVisitedCheckerSet {
	for _, c := range e.checkers {
		if checker.IsEqual(c.checker) {
			return c
		}
	}
	c := &matchVisitedCheckerSet{checker: checker}
	e.checkers = append(e.checkers, c)
	return c
}

// add adds a new entry to the queue.
func (queue *matchQueue) add(id storage.ID, checker *acl.Checker, ss forwardStateSet) {
	queue.queue = append(queue.queue, &matchQueueEntry{
		id:      id,
		checker: checker,
		ss:      ss,
	})
}

// isEmpty returns true iff the queue is empty.
func (queue *matchQueue) isEmpty() bool {
	return len(queue.queue) == 0
}

// pop removes an entry from the queue.
func (queue *matchQueue) pop() *matchQueueEntry {
	i := len(queue.queue)
	qentry := queue.queue[i-1]
	queue.queue = queue.queue[:i-1]
	return qentry
}

// step advances the automaton for all components of the path.
func (ss reverseStateSet) step(p *refs.Path) reverseStateSet {
	for p != nil {
		var s string
		p, s = p.Split()
		ss = reverseStateSet{StateSet: ss.Step(s)}
	}
	return ss
}

// step advances the automaton for all components of the path.
func (ss forwardStateSet) step(p *refs.Path) forwardStateSet {
	if p == nil {
		return ss
	}
	p, s := p.Split()
	return forwardStateSet{StateSet: ss.step(p).Step(s)}
}
