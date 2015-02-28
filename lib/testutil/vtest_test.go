package testutil

import (
	"testing"
)

func TestCallAndRecover(t *testing.T) {
	tests := []struct {
		f      func()
		expect interface{}
	}{
		{func() {}, nil},
		{func() { panic(nil) }, nil},
		{func() { panic(123) }, 123},
		{func() { panic("abc") }, "abc"},
	}
	for _, test := range tests {
		got := CallAndRecover(test.f)
		if got != test.expect {
			t.Errorf(`CallAndRecover got "%v", want "%v"`, got, test.expect)
		}
	}
}
