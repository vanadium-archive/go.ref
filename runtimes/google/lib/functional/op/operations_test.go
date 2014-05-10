package op

import (
	"testing"

	"veyron/runtimes/google/lib/functional"
	"veyron/runtimes/google/lib/functional/rb"
)

func intCompare(it1, it2 interface{}) bool {
	return it1.(int) < it2.(int)
}

func difference(s1, s2 functional.Set) functional.Set {
	difference := rb.NewSet(intCompare)
	IterDifference(s1, s2, func(it interface{}) bool {
		difference = difference.Put(it)
		return true
	})
	return difference
}

func TestDifferenceEmpty(t *testing.T) {
	s1 := rb.NewSet(intCompare)
	s2 := rb.NewSet(intCompare)
	difference := difference(s1, s2)
	if !difference.IsEmpty() {
		t.Fatal("Difference is not empty")
	}
}

func TestDifferenceDisjoint(t *testing.T) {
	s1 := rb.NewSet(intCompare).Put(1).Put(2).Put(3)
	s2 := rb.NewSet(intCompare).Put(4).Put(5)
	difference := difference(s1, s2)
	if difference.Len() != 3 {
		t.Fatalf("Difference does not contain 3 elements ")
	}
	for i := 1; i <= 3; i++ {
		if !difference.Contains(i) {
			t.Fatalf("Difference does not contain %v", i)
		}
	}
}

func TestDifferenceOverlapping(t *testing.T) {
	s1 := rb.NewSet(intCompare).Put(1).Put(2).Put(3).Put(5)
	s2 := rb.NewSet(intCompare).Put(4).Put(5)
	difference := difference(s1, s2)
	if difference.Len() != 3 {
		t.Fatalf("Difference does not contain 3 elements ")
	}
	for i := 1; i <= 3; i++ {
		if !difference.Contains(i) {
			t.Fatalf("Difference does not contain %v", i)
		}
	}
}
