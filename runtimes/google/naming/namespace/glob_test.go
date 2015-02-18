package namespace

import (
	"testing"

	"v.io/core/veyron2/security"
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
	const notset = ""
	cases := []struct {
		input      string
		mt, server security.BlessingPattern
		name       string
	}{
		{"[foo/bar]", notset, "foo/bar", ""},
		{"[x/y]/", "x/y", notset, "/"},
		{"[foo]a", notset, "foo", "a"},
		{"[foo]/a", "foo", notset, "/a"},
		{"[foo]/a/[bar]", "foo", "bar", "/a"},
		{"a/b", notset, notset, "a/b"},
		{"[foo]a/b", notset, "foo", "a/b"},
		{"/a/b", notset, notset, "/a/b"},
		{"[foo]/a/b", "foo", notset, "/a/b"},
		{"/a/[bar]b", notset, "bar", "/a/b"},
		{"[foo]/a/[bar]b", "foo", "bar", "/a/b"},
		{"/a/b[foo]", notset, notset, "/a/b[foo]"},
		{"/a/b/[foo]c", notset, notset, "/a/b/[foo]c"},
		{"/[01:02::]:444", notset, notset, "/[01:02::]:444"},
		{"[foo]/[01:02::]:444", "foo", notset, "/[01:02::]:444"},
		{"/[01:02::]:444/foo", notset, notset, "/[01:02::]:444/foo"},
		{"[a]/[01:02::]:444/foo", "a", notset, "/[01:02::]:444/foo"},
		{"/[01:02::]:444/[b]foo", notset, "b", "/[01:02::]:444/foo"},
		{"[c]/[01:02::]:444/[d]foo", "c", "d", "/[01:02::]:444/foo"},
	}
	for _, c := range cases {
		mt, server, name := splitObjectName(c.input)
		if mt != c.mt {
			t.Errorf("%q: unexpected mt pattern: %q not %q", c.input, mt, c.mt)
		}
		if server != c.server {
			t.Errorf("%q: unexpected server pattern: %q not %q", c.input, server, c.server)
		}
		if name != c.name {
			t.Errorf("%q: unexpected name: %q not %q", c.input, name, c.name)
		}
	}
}
