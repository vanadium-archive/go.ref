package rb

import (
	"fmt"
	"log"
	"testing"

	"veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/runtimes/google/lib/functional"
)

const (
	// Number of elements to check
	kMaxElement = 100

	// Number of times to loop during the random test
	kLoopCount = 10000
)

// Invariant checking.
func checkInvariants(t *testing.T, s functional.Set) {
	set := s.(*rbSet)
	b := checkRedInvariant(t, set.root) &&
		checkRootInvariant(t, set.root) &&
		checkBlackInvariant(t, set.root)
	if !b {
		drawTree("", set.root)
	}
}

func checkRedInvariant(t *testing.T, node *node) bool {
	if node == nil {
		return true
	}
	if node.color == RED && (node.left != nil && node.left.color == RED ||
		node.right != nil && node.right.color == RED) {
		t.Errorf("RedBlackSet: RED invariant violation")
		return false
	}
	return checkRedInvariant(t, node.left) && checkRedInvariant(t, node.right)
}

func checkRootInvariant(t *testing.T, root *node) bool {
	if root != nil && root.color != BLACK {
		t.Errorf("RedBlackSet: Root is not BLACK")
		return false
	}
	return true
}

func checkBlackInvariant(t *testing.T, root *node) bool {
	depth := 0
	for node := root; node != nil; node = node.left {
		if node.color == BLACK {
			depth++
		}
	}
	b := checkBlackPathInvariant(depth, root)
	if !b {
		t.Error("Not all paths have the same number of black nodes")
	}
	return b
}

func checkBlackPathInvariant(expected int, node *node) bool {
	if node == nil {
		return expected == 0
	}
	if node.color == BLACK {
		expected--
	}
	return checkBlackPathInvariant(expected, node.left) && checkBlackPathInvariant(expected, node.right)
}

func (set *rbSet) printTree() {
	drawTree("x", set.root)
}

func drawTree(prefix string, node *node) {
	if node != nil {
		var color string
		if node.color == RED {
			color = "R"
		} else {
			color = "B"
		}
		log.Printf("%s %s %d", prefix, color, node.key)
		drawTree(prefix+"l", node.left)
		drawTree(prefix+"r", node.right)
	}
}

func intCompare(it1, it2 interface{}) bool {
	return it1.(int) < it2.(int)
}

func checkMembership(t *testing.T, s functional.Set, elements map[int]struct{}) {
	checkInvariants(t, s)

	s.Iter(func(it interface{}) bool {
		i := it.(int)
		if _, ok := elements[i]; !ok {
			t.Errorf("Extra element: %d", i)
		}
		return true
	})
	itElements := make(map[int]struct{})
	for it := s.Iterator(); it.IsValid(); it.Next() {
		i := it.Get().(int)
		if _, ok := elements[i]; !ok {
			t.Errorf("Extra element: %d", i)
		}
		itElements[i] = struct{}{}
	}
	for i, _ := range elements {
		if !s.Contains(i) {
			t.Errorf("Missing element: %d", i)
		}
		if j, ok := s.Get(i); !ok || j != i {
			t.Errorf("Expected %d, got %d", i, j)
		}
		if _, ok := itElements[i]; !ok {
			t.Errorf("Missing iterator element: %d", i)
		}
	}

	if s.Len() != len(elements) {
		t.Errorf("Expected size %d, actual size %d", len(elements), s.Len())
	}
}

// Sequential add and remove elements.
func TestAddRemove(t *testing.T) {
	s := NewSet(intCompare)
	if !s.IsEmpty() {
		t.Errorf("set should be empty: %v", s)
	}
	if s.Len() != 0 {
		t.Errorf("set should have size zero: %d", s.Len())
	}

	// Add some elements
	elements := make(map[int]struct{})
	for i := 0; i != kMaxElement; i++ {
		s = s.Put(i)
		elements[i] = struct{}{}
		checkMembership(t, s, elements)
	}

	// Remove some elements
	for i := kMaxElement - 1; i > 0; i-- {
		s = s.Remove(i)
		delete(elements, i)
		checkMembership(t, s, elements)
	}
}

// Randomized add and remove.
func TestRandom(t *testing.T) {
	s := NewSet(intCompare)
	elements := make(map[int]struct{})
	for i := 0; i != kLoopCount; i++ {
		switch testutil.Rand.Intn(2) {
		case 0:
			// Insertion
			x := testutil.Rand.Intn(kMaxElement)
			elements[x] = struct{}{}
			s = s.Put(x)
		case 1:
			// Deletion
			x := testutil.Rand.Intn(kMaxElement)
			delete(elements, x)
			s = s.Remove(x)
		}
		checkMembership(t, s, elements)
	}
}

// Map test.
type entry struct {
	key, value int
}

func entryLessThan(e1, e2 interface{}) bool {
	return e1.(*entry).key < e2.(*entry).key
}

func entryKeyFn(it interface{}) int {
	return it.(*entry).key
}

func entryValueFn(it interface{}) interface{} {
	return it.(*entry).value
}

func entryEntryFn(key int) interface{} {
	return &entry{key: key}
}

type entry2 struct {
	key   int
	value string
}

func entry2LessThan(e1, e2 interface{}) bool {
	return e1.(*entry2).key < e2.(*entry2).key
}

func entry2KeyFn(it interface{}) int {
	return it.(*entry2).key
}

func entry2ValueFn(it interface{}) interface{} {
	return it.(*entry2).value
}

func entry2EntryFn(key int) interface{} {
	return &entry2{key: key}
}

func checkMaps(t *testing.T, s functional.Set, elements map[int]interface{},
	keyFn func(interface{}) int,
	valueFn func(interface{}) interface{},
	entryFn func(int) interface{}) {
	checkInvariants(t, s)

	s.Iter(func(it interface{}) bool {
		key := keyFn(it)
		value := valueFn(it)
		if j, ok := elements[key]; !ok || j != value {
			t.Errorf("Expected %d, got %d", value, j)
		}
		return true
	})
	for i, x := range elements {
		if !s.Contains(entryFn(i)) {
			t.Errorf("Missing element: %d", i)
		}
		v, ok := s.Get(entryFn(i))
		if !ok {
			t.Errorf("Missing element: %d", i)
		} else {
			key := keyFn(v)
			value := valueFn(v)
			if key != i || value != x {
				t.Errorf("Expected (%d, %d), got (%v)", i, x, v)
			}
		}
	}

	if s.Len() != len(elements) {
		t.Errorf("Expected size %d, actual size %d", len(elements), s.Len())
	}
}

func TestSequentialMap(t *testing.T) {
	s := NewSet(entryLessThan)
	elements := make(map[int]interface{})
	for i := 0; i != kMaxElement; i++ {
		elements[i] = i + 1
		s = s.Put(&entry{key: i, value: i + 1})
		checkMaps(t, s, elements, entryKeyFn, entryValueFn, entryEntryFn)
	}

	for i := kMaxElement; i >= 0; i-- {
		delete(elements, i)
		s = s.Remove(&entry{key: i})
		checkMaps(t, s, elements, entryKeyFn, entryValueFn, entryEntryFn)
	}
}

func TestRandomMap(t *testing.T) {
	s := NewSet(entryLessThan)
	elements := make(map[int]interface{})
	for i := 0; i != kLoopCount; i++ {
		switch testutil.Rand.Intn(2) {
		case 0:
			// Insertion
			k := testutil.Rand.Intn(kMaxElement)
			v := testutil.Rand.Int()
			elements[k] = v
			s = s.Put(&entry{key: k, value: v})
		case 1:
			// Deletion
			k := testutil.Rand.Intn(kMaxElement)
			delete(elements, k)
			s = s.Remove(&entry{key: k})
		}
		checkMaps(t, s, elements, entryKeyFn, entryValueFn, entryEntryFn)
	}
}

func TestMapMap(t *testing.T) {
	s := NewSet(entryLessThan)
	elements := make(map[int]interface{})
	for i := 0; i != kMaxElement; i++ {
		elements[i] = i + 1
		s = s.Put(&entry{key: i, value: i + 1})
		checkMaps(t, s, elements, entryKeyFn, entryValueFn, entryEntryFn)
	}

	s2 := s.Map(func(it interface{}) interface{} {
		e := *it.(*entry)
		e.value++
		return &e
	}, entryLessThan)
	elements2 := make(map[int]interface{})
	for k, v := range elements {
		elements2[k] = v.(int) + 1
	}
	checkMaps(t, s2, elements2, entryKeyFn, entryValueFn, entryEntryFn)
}

func TestMapFold(t *testing.T) {
	s := NewSet(entryLessThan)
	elements := make(map[int]interface{})
	for i := 0; i != kMaxElement; i++ {
		elements[i] = i + 1
		s = s.Put(&entry{key: i, value: i + 1})
		checkMaps(t, s, elements, entryKeyFn, entryValueFn, entryEntryFn)
	}

	l := s.Fold(func(it, x interface{}) interface{} {
		l := x.([]entry)
		return append(l, *it.(*entry))
	}, ([]entry)(nil))

	s2 := NewSet(entryLessThan)
	for i, e := range l.([]entry) {
		e2 := e // Copy the entry.
		if e2.key != i || e2.value != i+1 {
			t.Errorf("Unexpected entry: %v", e2)
		}
		s2 = s2.Put(&e2)
	}
	checkMaps(t, s2, elements, entryKeyFn, entryValueFn, entryEntryFn)
}

func TestMapToNewType(t *testing.T) {
	s := NewSet(entryLessThan)
	elements := make(map[int]interface{})
	for i := 0; i != kMaxElement; i++ {
		elements[i] = i + 1
		s = s.Put(&entry{key: i, value: i + 1})
	}
	s2 := s.Map(func(it interface{}) interface{} {
		e := *it.(*entry)
		return &entry2{
			key:   e.key,
			value: fmt.Sprintf("s%v", e.value),
		}
	}, entry2LessThan)
	elements2 := make(map[int]interface{})
	for k, v := range elements {
		elements2[k] = fmt.Sprintf("s%v", v)
	}
	checkMaps(t, s2, elements2, entry2KeyFn, entry2ValueFn, entry2EntryFn)
}

////////////////////////////////////////////////////////////////////////
// Simple random benchmark

// Number of elements to check
var kMaxElementBench int = 1000

// Table of random ops
type opname int

const (
	CONTAINS opname = iota
	PUT
	REMOVE
	ITERATE
)

type operation struct {
	op    opname
	count int
}

var optable = [...]operation{
	operation{CONTAINS, 40},
	operation{PUT, 4},
	operation{REMOVE, 4},
	operation{ITERATE, 1}}

func makeOperations() []opname {
	totalsize := 0
	for i := 0; i != len(optable); i++ {
		totalsize += optable[i].count
	}

	operations := make([]opname, totalsize)
	index := 0
	for i := 0; i != len(optable); i++ {
		op := optable[i]
		for j := 0; j != op.count; j++ {
			operations[index] = op.op
			index++
		}
	}
	return operations
}

func BenchmarkRandom(b *testing.B) {
	operations := makeOperations()
	s := NewSet(intCompare)

	for i := 0; i != b.N; i++ {
		switch operations[testutil.Rand.Intn(len(operations))] {
		case CONTAINS:
			s.Contains(testutil.Rand.Intn(kMaxElement))

		case PUT:
			s = s.Put(testutil.Rand.Intn(kMaxElement))

		case REMOVE:
			s = s.Remove(testutil.Rand.Intn(kMaxElement))

		case ITERATE:
			s.Iter(func(it interface{}) bool { return true })
		}
	}
}
