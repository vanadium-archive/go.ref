package glob

import (
	"testing"
)

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestStripFixedPrefix(t *testing.T) {
	tests := []struct {
		pattern string
		fixed   []string
		rest    string
	}{
		{"*", nil, "*"},
		{"a/b/c/*", []string{"a", "b", "c"}, "*"},
		{"a/b/*/...", []string{"a", "b"}, "*/..."},
		{"a/b/c/...", []string{"a", "b", "c"}, "..."},
		{"a/the\\?rain.in\\*spain", []string{"a", "the?rain.in*spain"}, ""},
	}
	for _, test := range tests {
		g, err := Parse(test.pattern)
		if err != nil {
			t.Fatalf("parsing %q: %q", test.pattern, err.Error())
		}
		if f, ng := g.SplitFixedPrefix(); !same(f, test.fixed) || test.rest != ng.String() {
			t.Fatalf("SplitFixedPrefix(%q) got %q,%q, expected %q,%q", test.pattern, f, ng.String(), test.fixed, test.rest)
		}
	}
}
