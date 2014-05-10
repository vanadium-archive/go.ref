/*
A package that implements topological sort.   For details see:
http://en.wikipedia.org/wiki/Topological_sorting
*/
package toposort

type Sorter interface {
	// AddNode adds a node value.  Arbitrary value types are supported, but the
	// values must be comparable - they'll be used as map keys.  Typically this is
	// only used to add potentially loner nodes with no incoming or outgoing edges.
	AddNode(value interface{})

	// AddEdge adds nodes corresponding to from and to, and adds an edge from -> to.
	// You don't need to call AddNode first; the nodes will be implicitly added if
	// they don't already exist.  The direction means that from depends on to;
	// i.e. to will appear before from in the sorted output.  Cycles are allowed.
	AddEdge(from, to interface{})

	// Sort returns the topologically sorted values, along with cycles which
	// represents some of the cycles (if any) that were encountered.  You're
	// guaranteed that len(cycles)==0 iff there are no cycles in the graph,
	// otherwise an arbitrary (but non-empty) list of cycles is returned.
	//
	// If there are cycles the sorting is best-effort; portions of the graph that
	// are acyclic will still be ordered correctly, and the cyclic portions have an
	// arbitrary ordering.
	//
	// Sort is deterministic; given the same sequence of inputs it will always
	// return the same output, even if the inputs are only partially ordered.  This
	// is useful for idl compilation where we re-order type definitions, and it's
	// nice to get a deterministic order.
	Sort() ([]interface{}, [][]interface{})
}

// NewSorter returns a new topological sorter.
func NewSorter() Sorter {
	return &topoSort{}
}

// topoSort performs a topological sort.  This is meant to be simple, not
// high-performance.  For details see:
// http://en.wikipedia.org/wiki/Topological_sorting
type topoSort struct {
	values map[interface{}]int
	nodes  []*topoNode
}

// topoNode is a node in the graph representing the topological information.
type topoNode struct {
	index    int
	value    interface{}
	children []*topoNode
}

func (tn *topoNode) addChild(child *topoNode) {
	tn.children = append(tn.children, child)
}

func (ts *topoSort) addOrGetNode(value interface{}) *topoNode {
	if ts.nodes == nil {
		ts.values = make(map[interface{}]int)
	}
	if index, ok := ts.values[value]; ok {
		return ts.nodes[index]
	}
	index := len(ts.nodes)
	newNode := &topoNode{index: index, value: value}
	ts.values[value] = index
	ts.nodes = append(ts.nodes, newNode)
	return newNode
}

// AddNode adds a node value.  Arbitrary value types are supported, but the
// values must be comparable - they'll be used as map keys.  Typically this is
// only used to add potentially loner nodes with no incoming or outgoing edges.
func (ts *topoSort) AddNode(value interface{}) {
	ts.addOrGetNode(value)
}

// AddEdge adds nodes corresponding to from and to, and adds an edge from -> to.
// You don't need to call AddNode first; the nodes will be implicitly added if
// they don't already exist.  The direction means that from depends on to;
// i.e. to will appear before from in the sorted output.  Cycles are allowed.
func (ts *topoSort) AddEdge(from interface{}, to interface{}) {
	fromNode := ts.addOrGetNode(from)
	toNode := ts.addOrGetNode(to)
	fromNode.addChild(toNode)
}

// Sort returns the topologically sorted values, along with cycles which
// represents some of the cycles (if any) that were encountered.  You're
// guaranteed that len(cycles)==0 iff there are no cycles in the graph,
// otherwise an arbitrary (but non-empty) list of cycles is returned.
//
// If there are cycles the sorting is best-effort; portions of the graph that
// are acyclic will still be ordered correctly, and the cyclic portions have an
// arbitrary ordering.
//
// Sort is deterministic; given the same sequence of inputs it will always
// return the same output, even if the inputs are only partially ordered.  This
// is useful for idl compilation where we re-order type definitions, and it's
// nice to get a deterministic order.
func (ts *topoSort) Sort() (sorted []interface{}, cycles [][]interface{}) {
	// The strategy is the standard simple approach of performing DFS on the
	// graph.  Details are outlined in the above wikipedia article.
	done := make(topoNodeSet)
	for _, node := range ts.nodes {
		cycles = appendCycles(cycles, node.visit(done, make(topoNodeSet), &sorted))
	}
	return
}

type topoNodeSet map[*topoNode]struct{}

// visit performs DFS on the graph, and fills in sorted and cycles as it
// traverses.  We use done to indicate a node has been fully explored, and
// visiting to indicate a node is currently being explored.
//
// The cycle collection strategy is to wait until we've hit a repeated node in
// visiting, and add that node to cycles and return.  Thereafter as the
// recursive stack is unwound, nodes append themselves to the end of each cycle,
// until we're back at the repeated node.  This guarantees that if the graph is
// cyclic we'll return at least one of the cycles.
func (tn *topoNode) visit(done, visiting topoNodeSet, sorted *[]interface{}) (cycles [][]interface{}) {
	if _, ok := done[tn]; ok {
		return
	}
	if _, ok := visiting[tn]; ok {
		cycles = [][]interface{}{{tn.value}}
		return
	}
	visiting[tn] = struct{}{}
	for _, child := range tn.children {
		cycles = appendCycles(cycles, child.visit(done, visiting, sorted))
	}
	done[tn] = struct{}{}
	*sorted = append(*sorted, tn.value)
	// Update cycles.  If it's empty none of our children detected any cycles, and
	// there's nothing to do.  Otherwise we append ourselves to the cycle, iff the
	// cycle hasn't completed yet.  We know the cycle has completed if the first
	// and last item in the cycle are the same, with an exception for the single
	// item case; self-cycles are represented as the same node appearing twice.
	for cx := range cycles {
		len := len(cycles[cx])
		if len == 1 || cycles[cx][0] != cycles[cx][len-1] {
			cycles[cx] = append(cycles[cx], tn.value)
		}
	}
	return
}

// appendCycles returns the combined cycles in a and b.
func appendCycles(a [][]interface{}, b [][]interface{}) [][]interface{} {
	for _, bcycle := range b {
		a = append(a, bcycle)
	}
	return a
}

// PrintCycles prints the cycles returned from topoSort.Sort, using f to convert
// each node into a string.
func PrintCycles(cycles [][]interface{}, f func(n interface{}) string) (str string) {
	for cyclex, cycle := range cycles {
		if cyclex > 0 {
			str += " "
		}
		str += "["
		for nodex, node := range cycle {
			if nodex > 0 {
				str += " <= "
			}
			str += f(node)
		}
		str += "]"
	}
	return
}
