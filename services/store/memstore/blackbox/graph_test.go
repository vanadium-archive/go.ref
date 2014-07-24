package blackbox

import (
	"testing"

	_ "veyron/lib/testutil"
	"veyron/services/store/memstore"

	"veyron2/storage"
)

// A Node has a Label and some Children.
type Node struct {
	Label    string
	Children map[string]storage.ID
}

// Create a linear graph and truncate it.
func TestLinear(t *testing.T) {
	const linearNodeCount = 10

	st, err := memstore.New(rootPublicID, "")
	if err != nil {
		t.Fatalf("memstore.New() failed: %v", err)
	}
	if v, err := st.Bind("/").Get(rootPublicID, nil); v != nil || err == nil {
		t.Errorf("Unexpected root")
	}

	if _, err := st.Bind("/").Put(rootPublicID, nil, &Node{}); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	// Create a linked list.
	path := ""
	var nodes [linearNodeCount]*Node
	var ids [linearNodeCount]storage.ID
	tr := memstore.NewTransaction()
	for i := 0; i != linearNodeCount; i++ {
		path = path + "/Children/a"
		node := &Node{Label: path}
		nodes[i] = node
		stat, err := st.Bind(path).Put(rootPublicID, tr, node)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		ids[i] = stat.ID
		if _, err := st.Bind(path).Get(rootPublicID, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}
	tr.Commit()

	// Verify that all the nodes still exist.
	st.GC()
	for i, node := range nodes {
		path := node.Label
		id := ids[i]
		ExpectExists(t, st, path, id)
	}

	// Truncate the graph to length 3.
	{
		node := nodes[2]
		node.Children = nil
		if _, err := st.Bind("/Children/a/Children/a/Children/a").Put(rootPublicID, nil, node); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	st.GC()
	for i, node := range nodes {
		path := node.Label
		id := ids[i]
		if i < 3 {
			ExpectExists(t, st, path, id)
		} else {
			ExpectNotExists(t, st, path, id)
		}
	}
}

// Create a lollipop graph and remove part of the cycle.
func TestLollipop(t *testing.T) {
	const linearNodeCount = 10
	const loopNodeIndex = 5
	const cutNodeIndex = 7

	st, err := memstore.New(rootPublicID, "")
	if err != nil {
		t.Fatalf("memstore.New() failed: %v", err)
	}
	if v, err := st.Bind("/").Get(rootPublicID, nil); v != nil || err == nil {
		t.Errorf("Unexpected root")
	}

	stat, err := st.Bind("/").Put(rootPublicID, nil, &Node{})
	if err != nil || stat == nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	id := stat.ID

	// Create a linked list.
	path := ""
	var nodes [linearNodeCount]*Node
	var ids [linearNodeCount]storage.ID
	tr := memstore.NewTransaction()
	for i := 0; i != linearNodeCount; i++ {
		path = path + "/Children/a"
		node := &Node{Label: path}
		nodes[i] = node
		stat, err := st.Bind(path).Put(rootPublicID, tr, node)
		if err != nil || stat == nil {
			t.Errorf("Unexpected error: %s: %s", path, err)
		}
		id = stat.ID
		ids[i] = id
	}

	// Add a back-loop.
	{
		node := nodes[linearNodeCount-1]
		node.Children = map[string]storage.ID{"a": ids[loopNodeIndex]}
		if _, err := st.Bind(node.Label).Put(rootPublicID, tr, node); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}
	tr.Commit()

	// Verify that all the nodes still exist.
	st.GC()
	for i, node := range nodes {
		path := node.Label
		id := ids[i]
		ExpectExists(t, st, path, id)
	}

	// Truncate part of the loop.
	{
		node := nodes[cutNodeIndex]
		node.Children = nil
		if _, err := st.Bind(node.Label).Put(rootPublicID, nil, node); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	st.GC()
	for i, node := range nodes {
		path := node.Label
		id := ids[i]
		if i <= cutNodeIndex {
			ExpectExists(t, st, path, id)
		} else {
			ExpectNotExists(t, st, path, id)
		}
	}
}
