package query

import (
	"testing"
)

func TestMonotonic(t *testing.T) {
	mi := monotonicInt{}
	a := mi.Next()
	for i := 0; i < 5; i++ {
		b := mi.Next()
		if b <= a {
			t.Fatalf("%d is not greater than %d", b, a)
		}
		a = b
	}
}
