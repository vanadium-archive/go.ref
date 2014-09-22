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

func TestExactMatch(t *testing.T) {
	tests := []struct {
		pattern string
		elems   []string
		matched bool
		exact   bool
	}{
		// Test one element, fixed.
		{"a", []string{"a"}, true, true},
		{"a", []string{"b"}, false, false},
		{"\\\\", []string{"\\"}, true, true},
		// Test one element, containing *.
		{"*", []string{"*"}, true, false},
		{"*", []string{"abc"}, true, false},
		{"\\*", []string{"*"}, true, true},
		{"\\*", []string{"abc"}, false, false},
		{"\\\\*", []string{"\\"}, true, false},
		{"\\\\*", []string{"\\*"}, true, false},
		{"\\\\*", []string{"\\abc"}, true, false},
		// Test one element, containing ?.
		{"?", []string{"?"}, true, false},
		{"?", []string{"a"}, true, false},
		{"\\?", []string{"?"}, true, true},
		{"\\?", []string{"a"}, false, false},
		{"\\\\?", []string{"\\"}, false, false},
		{"\\\\?", []string{"\\?"}, true, false},
		{"\\\\?", []string{"\\a"}, true, false},
		// Test one element, containing [].
		{"[abc]", []string{"c"}, true, false},
		{"\\[abc\\]", []string{"[abc]"}, true, true},
		{"\\[abc\\]", []string{"a"}, false, false},
		{"\\\\[abc]", []string{"\\a"}, true, false},
		{"\\\\[abc]", []string{"a"}, false, false},
		{"[\\\\]", []string{"\\"}, true, false},
		{"[\\\\]", []string{"a"}, false, false},
		// Test multiple elements.
		{"a/b", []string{"a", "b"}, true, true},
		{"a/\\*", []string{"a", "*"}, true, true},
		{"a/\\?", []string{"a", "?"}, true, true},
		{"a/\\[b\\]", []string{"a", "[b]"}, true, true},
		{"a/*", []string{"a", "bc"}, true, false},
		{"a/?", []string{"a", "b"}, true, false},
		{"a/[bc]", []string{"a", "b"}, true, false},
		{"a/*/c", []string{"a", "b", "c"}, true, false},
		{"a/?/c", []string{"a", "b", "c"}, true, false},
		{"a/[bc]/d", []string{"a", "b", "d"}, true, false},
	}
	for _, test := range tests {
		g, err := Parse(test.pattern)
		if err != nil {
			t.Fatalf("parsing %q: %q", test.pattern, err.Error())
		}
		matched, exact, _ := g.PartialMatch(0, test.elems)
		if matched != test.matched || exact != test.exact {
			t.Fatalf("%v.PartialMatch(0, %v) got (%v, %v), expected (%v, %v)",
				test.pattern, test.elems, matched, exact, test.matched, test.exact)
		}
	}
}
