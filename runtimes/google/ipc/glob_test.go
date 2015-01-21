package ipc_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"

	"v.io/core/veyron/lib/glob"
	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/profiles"
)

func startServer(ctx *context.T, tree *node) (string, func(), error) {
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("failed to start debug server: %v", err)
	}
	endpoints, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen: %v", err)
	}
	if err := server.ServeDispatcher("", &disp{tree}); err != nil {
		return "", nil, err
	}
	ep := endpoints[0].String()
	return ep, func() { server.Stop() }, nil
}

func TestGlob(t *testing.T) {
	runtime, err := rt.New()
	if err != nil {
		panic(err)
	}
	defer runtime.Cleanup()
	ctx := runtime.NewContext()
	if ctx, err = veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-blessing")); err != nil {
		t.Fatal(err)
	}

	namespace := []string{
		"a/b/c1/d1",
		"a/b/c1/d2",
		"a/b/c2/d1",
		"a/b/c2/d2",
		"a/x/y/z",
		"leaf",
	}
	tree := newNode()
	for _, p := range namespace {
		tree.find(strings.Split(p, "/"), true)
	}

	ep, stop, err := startServer(ctx, tree)
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
			"leaf",
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
		{"", "*", []string{"a", "leaf"}},
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
		{"leaf", "", []string{""}},
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
		name := naming.JoinAddressName(ep, tc.name)
		results, err := testutil.GlobName(ctx, name, tc.pattern)
		if err != nil {
			t.Errorf("unexpected Glob error for (%q, %q): %v", tc.name, tc.pattern, err)
			continue
		}
		if !reflect.DeepEqual(results, tc.expected) {
			t.Errorf("unexpected result for (%q, %q). Got %q, want %q", tc.name, tc.pattern, results, tc.expected)
		}
	}
}

type disp struct {
	tree *node
}

func (d *disp) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	elems := strings.Split(suffix, "/")
	if len(elems) != 0 && elems[0] == "muah" {
		// Infinite space. Each node has one child named "ha".
		return ipc.ChildrenGlobberInvoker("ha"), nil, nil

	}
	if len(elems) != 0 && elems[0] == "leaf" {
		return leafObject{}, nil, nil
	}
	if len(elems) < 2 || (elems[0] == "a" && elems[1] == "x") {
		return &vChildrenObject{d.tree, elems}, nil, nil
	}
	return &globObject{d.tree, elems}, nil, nil
}

type globObject struct {
	n      *node
	suffix []string
}

func (o *globObject) Glob__(ctx ipc.ServerContext, pattern string) (<-chan naming.VDLMountEntry, error) {
	g, err := glob.Parse(pattern)
	if err != nil {
		return nil, err
	}
	n := o.n.find(o.suffix, false)
	if n == nil {
		return nil, nil
	}
	ch := make(chan naming.VDLMountEntry)
	go func() {
		o.globLoop(ch, "", g, n)
		close(ch)
	}()
	return ch, nil
}

func (o *globObject) globLoop(ch chan<- naming.VDLMountEntry, name string, g *glob.Glob, n *node) {
	if g.Len() == 0 {
		ch <- naming.VDLMountEntry{Name: name}
	}
	if g.Finished() {
		return
	}
	for leaf, child := range n.children {
		if ok, _, left := g.MatchInitialSegment(leaf); ok {
			o.globLoop(ch, naming.Join(name, leaf), left, child)
		}
	}
}

type vChildrenObject struct {
	n      *node
	suffix []string
}

func (o *vChildrenObject) GlobChildren__(ipc.ServerContext) (<-chan string, error) {
	n := o.n.find(o.suffix, false)
	if n == nil {
		return nil, fmt.Errorf("object does not exist")
	}
	ch := make(chan string, len(n.children))
	for child, _ := range n.children {
		ch <- child
	}
	close(ch)
	return ch, nil
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

type leafObject struct{}

func (l leafObject) Func(call ipc.ServerContext) error {
	return nil
}
