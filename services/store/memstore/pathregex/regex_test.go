package pathregex

import (
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func pathMatch(re regex, s string) bool {
	var path []string
	if s != "" {
		path = strings.Split(s, "/")
	}
	ss := compileNFA(re)
	for _, s := range path {
		ss = ss.Step(s)
	}
	return ss.IsFinal()
}

func expectMatch(t *testing.T, re regex, s string) {
	if !pathMatch(re, s) {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): expected match: %s, %q", file, line, re, s)
	}
}

func expectNoMatch(t *testing.T, re regex, s string) {
	if pathMatch(re, s) {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): unexpected match: %s, %q", file, line, re, s)
	}
}

func TestSingle(t *testing.T) {
	p, err := regexp.Compile("a*bc*")
	if err != nil {
		t.Fatalf("Bad regular expression: %s", err)
	}
	re := newSingle(p)
	expectNoMatch(t, re, "")
	expectMatch(t, re, "b")
	expectMatch(t, re, "aabccc")
	expectNoMatch(t, re, "aabccc/b")
}

func TestSequence(t *testing.T) {
	re := newSequence([]regex{
		newSingle(regexp.MustCompile("abc")),
		newSingle(regexp.MustCompile("def")),
	})
	expectNoMatch(t, re, "")
	expectNoMatch(t, re, "abcdef")
	expectMatch(t, re, "abc/def")
	expectNoMatch(t, re, "abc/def/ghi")
}

func TestAlt(t *testing.T) {
	re := newAlt([]regex{
		newSingle(regexp.MustCompile("abc")),
		newSingle(regexp.MustCompile("def")),
	})
	expectNoMatch(t, re, "")
	expectMatch(t, re, "abc")
	expectMatch(t, re, "def")
	expectNoMatch(t, re, "x/abc")
}

func TestStar(t *testing.T) {
	re := newStar(newSingle(regexp.MustCompile("abc")))
	expectMatch(t, re, "")
	expectMatch(t, re, "abc")
	expectMatch(t, re, "abc/abc")
	expectNoMatch(t, re, "abc/abc/a/abc")
}

func TestComplex(t *testing.T) {
	// Case-insensitive match on (j/a/s/o/n)*.
	var rl [5]regex
	for i, s := range []string{"j", "a", "s", "o", "n"} {
		rl[i] = newAlt([]regex{
			newSingle(regexp.MustCompile(s)),
			newSingle(regexp.MustCompile(strings.ToUpper(s))),
		})
	}
	re := newStar(newSequence(rl[:]))
	expectMatch(t, re, "")
	expectMatch(t, re, "j/A/s/O/n")
	expectNoMatch(t, re, "j/A/O/s/n")
	expectMatch(t, re, "j/A/s/O/n/j/a/S/o/N")
}
