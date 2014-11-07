package ipc_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mounttable"
	"veyron.io/veyron/veyron2/services/mounttable/types"

	"veyron.io/veyron/veyron/lib/glob"
	"veyron.io/veyron/veyron/profiles"
)

func startServer(rt veyron2.Runtime, tree *node) (string, func(), error) {
	server, err := rt.NewServer()
	if err != nil {
		return "", nil, fmt.Errorf("failed to start debug server: %v", err)
	}
	endpoint, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen: %v", err)
	}
	if err := server.ServeDispatcher("", &disp{tree}); err != nil {
		return "", nil, err
	}
	ep := endpoint.String()
	return ep, func() { server.Stop() }, nil
}

func TestGlob(t *testing.T) {
	runtime := rt.Init()
	defer runtime.Cleanup()

	namespace := []string{
		"a/b/c1/d1",
		"a/b/c1/d2",
		"a/b/c2/d1",
		"a/b/c2/d2",
		"a/x/y/z",
	}
	tree := newNode()
	for _, p := range namespace {
		tree.find(strings.Split(p, "/"), true)
	}

	ep, stop, err := startServer(runtime, tree)
	if err != nil {
		t.Fatalf("startServer: %v", err)
	}
	defer stop()

	testcases := []struct {
		name, pattern string
		expected      []string
	}{
		{"", "...", []string{
			"",
			"a",
			"a/b",
			"a/b/c1",
			"a/b/c1/d1",
			"a/b/c1/d2",
			"a/b/c2",
			"a/b/c2/d1",
			"a/b/c2/d2",
			"a/x",
			"a/x/y",
			"a/x/y/z",
		}},
		{"a", "...", []string{
			"",
			"b",
			"b/c1",
			"b/c1/d1",
			"b/c1/d2",
			"b/c2",
			"b/c2/d1",
			"b/c2/d2",
			"x",
			"x/y",
			"x/y/z",
		}},
		{"a/b", "...", []string{
			"",
			"c1",
			"c1/d1",
			"c1/d2",
			"c2",
			"c2/d1",
			"c2/d2",
		}},
		{"a/b/c1", "...", []string{
			"",
			"d1",
			"d2",
		}},
		{"a/b/c1/d1", "...", []string{
			"",
		}},
		{"a/x", "...", []string{
			"",
			"y",
			"y/z",
		}},
		{"a/x/y", "...", []string{
			"",
			"z",
		}},
		{"a/x/y/z", "...", []string{
			"",
		}},
		{"", "", []string{""}},
		{"", "*", []string{"a"}},
		{"a", "", []string{""}},
		{"a", "*", []string{"b", "x"}},
		{"a/b", "", []string{""}},
		{"a/b", "*", []string{"c1", "c2"}},
		{"a/b/c1", "", []string{""}},
		{"a/b/c1", "*", []string{"d1", "d2"}},
		{"a/b/c1/d1", "*", []string{}},
		{"a/b/c1/d1", "", []string{""}},
		{"a", "*/c?", []string{"b/c1", "b/c2"}},
		{"a", "*/*", []string{"b/c1", "b/c2", "x/y"}},
		{"a", "*/*/*", []string{"b/c1/d1", "b/c1/d2", "b/c2/d1", "b/c2/d2", "x/y/z"}},
		{"a/x", "*/*", []string{"y/z"}},
		{"bad", "", []string{}},
		{"a/bad", "", []string{}},
		{"a/b/bad", "", []string{}},
		{"a/b/c1/bad", "", []string{}},
		{"a/x/bad", "", []string{}},
		{"a/x/y/bad", "", []string{}},
		// muah is an infinite space to test rescursion limit.
		{"muah", "*", []string{"ha"}},
		{"muah", "*/*", []string{"ha/ha"}},
		{"muah", "*/*/*/*/*/*/*/*/*/*/*/*", []string{"ha/ha/ha/ha/ha/ha/ha/ha/ha/ha/ha/ha"}},
		{"muah", "...", []string{
			"",
			"ha",
			"ha/ha",
			"ha/ha/ha",
			"ha/ha/ha/ha",
			"ha/ha/ha/ha/ha",
			"ha/ha/ha/ha/ha/ha",
			"ha/ha/ha/ha/ha/ha/ha",
			"ha/ha/ha/ha/ha/ha/ha/ha",
			"ha/ha/ha/ha/ha/ha/ha/ha/ha",
			"ha/ha/ha/ha/ha/ha/ha/ha/ha/ha",
		}},
	}
	for _, tc := range testcases {
		c := mounttable.GlobbableClient(naming.JoinAddressName(ep, tc.name))

		stream, err := c.Glob(runtime.NewContext(), tc.pattern)
		if err != nil {
			t.Fatalf("Glob failed: %v", err)
		}
		results := []string{}
		iterator := stream.RecvStream()
		for iterator.Advance() {
			results = append(results, iterator.Value().Name)
		}
		sort.Strings(results)
		if !reflect.DeepEqual(results, tc.expected) {
			t.Errorf("unexpected result for (%q, %q). Got %q, want %q", tc.name, tc.pattern, results, tc.expected)
		}
		if err := iterator.Err(); err != nil {
			t.Errorf("unexpected stream error for %q: %v", tc.name, err)
		}
		if err := stream.Finish(); err != nil {
			t.Errorf("Finish failed for %q: %v", tc.name, err)
		}
	}
}

type disp struct {
	tree *node
}

func (d *disp) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	elems := strings.Split(suffix, "/")
	if len(elems) != 0 && elems[0] == "muah" {
		// Infinite space. Each node has one child named "ha".
		return ipc.VChildrenGlobberInvoker("ha"), nil, nil

	}
	if len(elems) <= 2 || (elems[0] == "a" && elems[1] == "x") {
		return &vChildrenObject{d.tree, elems}, nil, nil
	}
	return &globObject{d.tree, elems}, nil, nil
}

type globObject struct {
	n      *node
	suffix []string
}

func (o *globObject) Glob(call ipc.ServerCall, pattern string) error {
	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}
	n := o.n.find(o.suffix, false)
	if n == nil {
		return nil
	}
	o.globLoop(call, "", g, n)
	return nil
}

func (o *globObject) globLoop(call ipc.ServerCall, name string, g *glob.Glob, n *node) {
	if g.Len() == 0 {
		call.Send(types.MountEntry{Name: name})
	}
	if g.Finished() {
		return
	}
	for leaf, child := range n.children {
		if ok, _, left := g.MatchInitialSegment(leaf); ok {
			o.globLoop(call, naming.Join(name, leaf), left, child)
		}
	}
}

type vChildrenObject struct {
	n      *node
	suffix []string
}

func (o *vChildrenObject) VGlobChildren() ([]string, error) {
	n := o.n.find(o.suffix, false)
	if n == nil {
		return nil, fmt.Errorf("object does not exist")
	}
	children := make([]string, len(n.children))
	index := 0
	for child, _ := range n.children {
		children[index] = child
		index++
	}
	return children, nil
}

type node struct {
	children map[string]*node
}

func newNode() *node {
	return &node{make(map[string]*node)}
}

func (n *node) find(names []string, create bool) *node {
	if len(names) == 1 && names[0] == "" {
		return n
	}
	for {
		if len(names) == 0 {
			return n
		}
		if next, ok := n.children[names[0]]; ok {
			n = next
			names = names[1:]
			continue
		}
		if create {
			nn := newNode()
			n.children[names[0]] = nn
			n = nn
			names = names[1:]
			continue
		}
		return nil
	}
}
