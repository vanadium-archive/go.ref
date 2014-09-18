package op

import (
	"veyron.io/veyron/veyron/runtimes/google/lib/functional"
)

// IterDifference iterates through the difference of two sets, applying function f
// to each element that belongs to s1 but not s2. IterDifference terminates when
// all elements have been examined, or when the function returns false.
func IterDifference(s1, s2 functional.Set, f func(it interface{}) bool) {
	// TODO(tilaks): if s1 and s2 use the same comparator, iterate s1 and s2
	// by merging their iterators in O(len(s1) + len(s2)) steps. This is difficult
	// because comparators are functions, and not comparable. It is, however,
	// reasonable to iterate s2 and detect inversions under s1's comparator.
	s1.Iter(func(it interface{}) bool {
		if s2.Contains(it) {
			return true
		}
		return f(it)
	})
}
