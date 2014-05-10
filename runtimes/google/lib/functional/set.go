package functional

// Set is a collection of elements that supports operations for adding and
// removing elements, testing for membership, and enumerating the elements by
// iteration.
//
// By "functional," we mean that all methods act like mathematical functions,
// meaning that they return the same value on the same argument.  That's one way
// to put it, but the real consequence is that the set is immutable.  Instead of
// altering a set in-place, methods that perform alterations return a new set
// with the changes.  The argument set is unaffected.
type Set interface {
	// Contains returns true iff the element "x" is in the set.
	Contains(x interface{}) bool

	// Get returns the actual entry associated with an element.
	Get(x interface{}) (interface{}, bool)

	// Put adds the element "x" to the set.
	Put(x interface{}) Set

	// Remove deletes the element "x" from the set.
	Remove(x interface{}) Set

	// IsEmpty returns true iff the set has no elements.
	IsEmpty() bool

	// Len returns the number of elements in the set.
	Len() int

	// Iterator returns an iterator referring to the first element in the set,
	// if there is one.  If the set is empty, the iterator's IsValid method
	// returns false.  Note that the set is immutable, so the values returned
	// from iteration are fixed at the time the Iterator is created and there
	// are no concurrency issues.
	Iterator() Iterator

	// Iter iterates through the set, applying the function f to each element.
	// Iteration terminates when all elements have been examined, or when the
	// function returns false.  Iter is similar to Iterator, but it is included
	// to support standard functional idioms, similar to Map and Fold.
	Iter(f func(it interface{}) bool)

	// Map applies a function to each element of the set in order, replacing the
	// elements value. The ordering of the elements must be preserved under the
	// new comparator.
	//
	//    { e1, e2, ..., eN }.Map(f, cmp) = { f(e1), f(e2), ..., f(eN) }
	//
	// Typically, this is use in dictionaries to update the value part of a
	// key/value pair, keeping the key unchanged, so preserving the order.
	Map(f func(it interface{}) interface{}, cmp Comparator) Set

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
	Fold(f func(it, x interface{}) interface{}, x interface{}) interface{}
}

// Comparator is the type of ordering relations.  The function returns true iff
// the first argument is less than the second.
//
// The relation must be a strict weak order, which means that 1) it is a partial
// order, and 2) equality is transitive (a == b iff !(a < b) && !(b < a)).
type Comparator func(it1, it2 interface{}) bool

// Iterator is the type of iterators over set elements.
type Iterator interface {
	// IsValid returns true iff the iterator refers to a valid set element.
	IsValid() bool

	// Get returns the element that the iterator refers to.  Undefined if
	// IsValid is false.
	Get() interface{}

	// Next advances the iterator to the next element in the set.
	Next()
}
