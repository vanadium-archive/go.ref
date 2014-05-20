package mounttable

import (
	"testing"
)

func TestDepth(t *testing.T) {
	cases := []struct {
		name  string
		depth int
	}{
		{"", 0},
		{"foo", 1},
		{"foo/", 1},
		{"foo/bar", 2},
		{"foo//bar", 2},
		{"/foo/bar", 2},
		{"//", 0},
		{"//foo//bar", 2},
		{"/foo/bar//baz//baf/", 4},
	}
	for _, c := range cases {
		if got, want := depth(c.name), c.depth; want != got {
			t.Errorf("%q: unexpected depth: %d not %d", c.name, got, want)
		}
	}
}
