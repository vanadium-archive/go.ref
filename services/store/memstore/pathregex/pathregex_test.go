package pathregex_test

import (
	"runtime"
	"strings"
	"testing"

	"veyron/services/store/memstore/pathregex"
)

func pathMatch(ss pathregex.StateSet, s string) bool {
	var path []string
	if s != "" {
		path = strings.Split(s, "/")
	}
	for _, s := range path {
		ss = ss.Step(s)
	}
	return ss.IsFinal()
}

func expectMatch(t *testing.T, ss pathregex.StateSet, s string) {
	if !pathMatch(ss, s) {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): expected match: %q", file, line, s)
	}
}

func expectNoMatch(t *testing.T, ss pathregex.StateSet, s string) {
	if pathMatch(ss, s) {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): unexpected match: %q", file, line, s)
	}
}

func TestSingle(t *testing.T) {
	ss, err := pathregex.Compile("a*b*c")
	if err != nil {
		t.Fatalf("Bad regex: %s", err)
	}
	expectNoMatch(t, ss, "")
	expectNoMatch(t, ss, "b")
	expectMatch(t, ss, "aabccc")
	expectNoMatch(t, ss, "aabccc/b")
}

func TestSequence(t *testing.T) {
	ss, err := pathregex.Compile("abc/def")
	if err != nil {
		t.Fatalf("Bad regex: %s", err)
	}
	expectNoMatch(t, ss, "")
	expectNoMatch(t, ss, "abcdef")
	expectMatch(t, ss, "abc/def")
	expectNoMatch(t, ss, "abc/def/ghi")
}

func TestAlt(t *testing.T) {
	ss, err := pathregex.Compile("{abc,def}")
	if err != nil {
		t.Fatalf("Bad regex: %s", err)
	}
	expectNoMatch(t, ss, "")
	expectNoMatch(t, ss, "abcdef")
	expectMatch(t, ss, "abc")
	expectMatch(t, ss, "def")
	expectNoMatch(t, ss, "abc/def")
}

func TestAltPath(t *testing.T) {
	ss, err := pathregex.Compile("{abc/def,def/abc}")
	if err != nil {
		t.Fatalf("Bad regex: %s", err)
	}
	expectNoMatch(t, ss, "")
	expectNoMatch(t, ss, "abcdef")
	expectNoMatch(t, ss, "abc")
	expectNoMatch(t, ss, "def")
	expectMatch(t, ss, "abc/def")
	expectMatch(t, ss, "def/abc")
}

func TestUnanchored(t *testing.T) {
	ss, err := pathregex.Compile(".../abc/...")
	if err != nil {
		t.Fatalf("Bad regex: %s", err)
	}
	expectNoMatch(t, ss, "")
	expectMatch(t, ss, "abc")
	expectNoMatch(t, ss, "def")
	expectMatch(t, ss, "x/x/abc/x/x")
}

func TestStar(t *testing.T) {
	ss, err := pathregex.Compile("{,a,*/aa,*/*/aaa,*/*/*/aaaa}")
	if err != nil {
		t.Fatalf("Bad regex: %s", err)
	}
	expectMatch(t, ss, "")
	expectMatch(t, ss, "a")
	expectNoMatch(t, ss, "b/a")
	expectMatch(t, ss, "b/aa")
	expectMatch(t, ss, "c/b/aaa")
	expectMatch(t, ss, "d/c/b/aaaa")
	expectNoMatch(t, ss, "d/c/b/aaa")
}

func TestSequenceReverse(t *testing.T) {
	ss, err := pathregex.CompileReverse("abc/def")
	if err != nil {
		t.Fatalf("Bad regex: %s", err)
	}
	expectNoMatch(t, ss, "")
	expectNoMatch(t, ss, "abcdef")
	expectMatch(t, ss, "def/abc")
	expectNoMatch(t, ss, "def/abc/ghi")
}

func TestAltReverse(t *testing.T) {
	ss, err := pathregex.CompileReverse(".../123/{abc/def,g/*/i}/456/...")
	if err != nil {
		t.Fatalf("Bad regex: %s", err)
	}
	expectNoMatch(t, ss, "")
	expectNoMatch(t, ss, "123/abc/def/456")
	expectMatch(t, ss, "456/def/abc/123")
	expectMatch(t, ss, "x/y/456/i/am/g/123/z/w")
}
