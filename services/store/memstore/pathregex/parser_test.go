package pathregex

import (
	"runtime"
	"strings"
	"testing"
)

type testCase struct {
	// path expression.
	path string

	// expected regular expression afer parsing.
	expected string

	// expected reversed regular expression.
	reversed string
}

type errorCase struct {
	// path expression.
	path string

	// expected error position.
	pos int64
}

var (
	testCases = []testCase{
		testCase{"a", "^a$", "^a$"},
		testCase{"*", "^.*$", "^.*$"},
		testCase{"?", "^.$", "^.$"},
		testCase{".", "^\\.$", "^\\.$"},
		testCase{"[a]", "^[a]$", "^[a]$"},
		testCase{"[]]", "^[]]$", "^[]]$"},
		testCase{"[a-z]", "^[a-z]$", "^[a-z]$"},
		testCase{"[^a-z]", "^[^a-z]$", "^[^a-z]$"},
		testCase{
			"DSC[0-9][0-9][0-9][0-9].jpg",
			"^DSC[0-9][0-9][0-9][0-9]\\.jpg$",
			"^DSC[0-9][0-9][0-9][0-9]\\.jpg$",
		},
		testCase{"abc/def", "^abc$/^def$", "^def$/^abc$"},
		testCase{"abc{foo,bar}def", "^abc(foo|bar)def$", "^abc(foo|bar)def$"},
		testCase{"{abc,def}", "(^abc$|^def$)", "(^abc$|^def$)"},
		testCase{
			"abc/{bbb,ccc,ddd}/def",
			"^abc$/(^bbb$|(^ccc$|^ddd$))/^def$",
			"^def$/(^bbb$|(^ccc$|^ddd$))/^abc$",
		},
		testCase{
			".../abc/def/...",
			"(<true>)*/^abc$/^def$/(<true>)*",
			"(<true>)*/^def$/^abc$/(<true>)*",
		},
		testCase{
			"abc/{bbb,ccc}/ddd,eee",
			"^abc$/(^bbb$|^ccc$)/^ddd,eee$",
			"^ddd,eee$/(^bbb$|^ccc$)/^abc$",
		},
		testCase{
			"aaa/bbb{ccc,ddd}eee,fff/ggg",
			"^aaa$/^bbb(ccc|ddd)eee,fff$/^ggg$",
			"^ggg$/^bbb(ccc|ddd)eee,fff$/^aaa$",
		},
		testCase{
			"aaa/{bbb/ccc,ddd{eee,fff}ggg,ggg/{hhh,iii}/jjj}/kkk",
			"^aaa$/(^bbb$/^ccc$|(^ddd(eee|fff)ggg$|^ggg$/(^hhh$|^iii$)/^jjj$))/^kkk$",
			"^kkk$/(^ccc$/^bbb$|(^ddd(eee|fff)ggg$|^jjj$/(^hhh$|^iii$)/^ggg$))/^aaa$",
		},
	}

	errorCases = []errorCase{
		errorCase{"[", 1},
		errorCase{"[]", 2},
		errorCase{"{", 1},
		errorCase{"{,", 2},
		errorCase{"aaa{bbb/ccc}ddd", 8},
		errorCase{"aaa/bbb/ccc}", 11},
	}
)

func parsePath(t *testing.T, c *testCase) {
	p := &parser{reader: strings.NewReader(c.path)}
	re := p.parsePath()
	pos, _ := p.reader.Seek(0, 1)
	if p.isErrored || p.reader.Len() != 0 {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): syntax error at char %d: %q", file, line, pos, c.path)
	}
	s := re.String()
	if s != c.expected {
		t.Errorf("Expected %q, got %q", c.expected, s)
	}

	re.reverse()
	s = re.String()
	if s != c.reversed {
		t.Errorf("Expected %q, got %q", c.reversed, s)
	}
}

func parsePathError(t *testing.T, e *errorCase) {
	_, file, line, _ := runtime.Caller(1)
	p := &parser{reader: strings.NewReader(e.path)}
	p.parsePath()
	if p.reader.Len() != 0 {
		p.setError()
	}
	if !p.isErrored {
		t.Errorf("%s(%d): expected error: %q", file, line, e.path)
	}
	if pos, _ := p.reader.Seek(0, 1); pos != e.pos {
		t.Errorf("%s(%d): %q: expected error at pos %d, got %d", file, line, e.path, e.pos, pos)
	}
}

func TestParser(t *testing.T) {
	for _, c := range testCases {
		parsePath(t, &c)
	}
}

func TestParserErrors(t *testing.T) {
	for _, e := range errorCases {
		parsePathError(t, &e)
	}
}
