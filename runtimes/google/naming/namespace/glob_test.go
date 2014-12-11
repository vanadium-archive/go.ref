package namespace

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

func TestSplitObjectName(t *testing.T) {
	cases := []struct {
		input, mt, server, name string
	}{
		{"[foo/bar]", "", "foo/bar", ""},
		{"[x/y/...]/", "x/y/...", "", "/"},
		{"[foo]a", "", "foo", "a"},
		{"[foo]/a", "foo", "", "/a"},
		{"[foo]/a/[bar]", "foo", "bar", "/a"},
		{"a/b", "", "", "a/b"},
		{"[foo]a/b", "", "foo", "a/b"},
		{"/a/b", "", "", "/a/b"},
		{"[foo]/a/b", "foo", "", "/a/b"},
		{"/a/[bar]b", "", "bar", "/a/b"},
		{"[foo]/a/[bar]b", "foo", "bar", "/a/b"},
		{"/a/b[foo]", "", "", "/a/b[foo]"},
		{"/a/b/[foo]c", "", "", "/a/b/[foo]c"},
		{"/[01:02::]:444", "", "", "/[01:02::]:444"},
		{"[foo]/[01:02::]:444", "foo", "", "/[01:02::]:444"},
		{"/[01:02::]:444/foo", "", "", "/[01:02::]:444/foo"},
		{"[a]/[01:02::]:444/foo", "a", "", "/[01:02::]:444/foo"},
		{"/[01:02::]:444/[b]foo", "", "b", "/[01:02::]:444/foo"},
		{"[c]/[01:02::]:444/[d]foo", "c", "d", "/[01:02::]:444/foo"},
	}
	for _, c := range cases {
		mt, server, name := splitObjectName(c.input)
		if string(mt) != c.mt {
			t.Errorf("%q: unexpected mt pattern: %q not %q", c.input, mt, c.mt)
		}
		if string(server) != c.server {
			t.Errorf("%q: unexpected server pattern: %q not %q", c.input, server, c.server)
		}
		if name != c.name {
			t.Errorf("%q: unexpected name: %q not %q", c.input, name, c.name)
		}
	}
}
