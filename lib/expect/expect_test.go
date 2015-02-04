package expect_test

import (
	"bufio"
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"v.io/core/veyron/lib/expect"
)

func TestSimple(t *testing.T) {
	buf := []byte{}
	buffer := bytes.NewBuffer(buf)
	buffer.WriteString("bar\n")
	buffer.WriteString("baz\n")
	buffer.WriteString("oops\n")
	s := expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	s.Expect("bar")
	s.Expect("baz")
	if err := s.Error(); err != nil {
		t.Error(err)
	}
	// This will fail the test.
	s.Expect("not oops")
	if err := s.Error(); err == nil {
		t.Error("unexpected success")
	} else {
		t.Log(s.Error())
	}
	s.ExpectEOF()
}

func TestExpectf(t *testing.T) {
	buf := []byte{}
	buffer := bytes.NewBuffer(buf)
	buffer.WriteString("bar 22\n")
	s := expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	s.Expectf("bar %d", 22)
	if err := s.Error(); err != nil {
		t.Error(err)
	}
	s.ExpectEOF()
}

func TestEOF(t *testing.T) {
	buf := []byte{}
	buffer := bytes.NewBuffer(buf)
	buffer.WriteString("bar 22\n")
	buffer.WriteString("baz 22\n")
	s := expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	s.Expectf("bar %d", 22)
	s.ExpectEOF()
	if err := s.Error(); err == nil {
		t.Error("unexpected success")
	} else {
		t.Log(s.Error())
	}
}

func TestExpectRE(t *testing.T) {
	buf := []byte{}
	buffer := bytes.NewBuffer(buf)
	buffer.WriteString("bar=baz\n")
	buffer.WriteString("aaa\n")
	buffer.WriteString("bbb\n")
	s := expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	if got, want := s.ExpectVar("bar"), "baz"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	s.ExpectRE("zzz|aaa", -1)
	if err := s.Error(); err != nil {
		t.Error(err)
	}
	if got, want := s.ExpectRE("(.*)", -1), [][]string{{"bbb", "bbb"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := s.ExpectRE("(.*", -1), [][]string{{"bbb", "bbb"}}; !reflect.DeepEqual(got, want) {
		// this will have failed the test also.
		if err := s.Error(); err == nil || !strings.Contains(err.Error(), "error parsing regexp") {
			t.Errorf("missing or wrong error: %v", s.Error())
		}
	}
	s.ExpectEOF()
}

func TestExpectSetRE(t *testing.T) {
	buf := []byte{}
	buffer := bytes.NewBuffer(buf)
	buffer.WriteString("bar=baz\n")
	buffer.WriteString("abc\n")
	buffer.WriteString("def\n")
	buffer.WriteString("abc\n")
	s := expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	got := s.ExpectSetRE("^bar=.*$", "def$", "^abc$", "^a..$")
	if s.Error() != nil {
		t.Errorf("unexpected error: %s", s.Error())
	}
	want := [][]string{{"bar=baz"}, {"def"}, {"abc"}, {"abc"}}
	if !reflect.DeepEqual(got, want) {
		t.Error("unexpected result from ExpectSetRE, got %v, want %v", got, want)
	}
	buffer.WriteString("ooh\n")
	buffer.WriteString("aah\n")
	s.ExpectSetRE("bar=.*", "def")
	if got, want := s.Error(), "expect_test.go:108: found no match for \"bar=.*\""; got == nil || got.Error() != want {
		t.Errorf("got %v, want %q", got, want)
	}
	s.ExpectEOF()

	buf = []byte{}
	buffer = bytes.NewBuffer(buf)
	s = expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	buffer.WriteString("hello world\n")
	buffer.WriteString("this is a test\n")
	matches := s.ExpectSetRE("hello (world)", "this (is) (a|b) test")
	if want := [][]string{{"hello world", "world"}, {"this is a test", "is", "a"}}; !reflect.DeepEqual(want, matches) {
		t.Errorf("unexpected result from ExpectSetRE, got %v, want %v", matches, want)
	}
}

func TestExpectSetEventuallyRE(t *testing.T) {
	buf := []byte{}
	buffer := bytes.NewBuffer(buf)
	buffer.WriteString("bar=baz\n")
	buffer.WriteString("abc\n")
	buffer.WriteString("def\n")
	buffer.WriteString("abc\n")
	s := expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	s.SetVerbosity(testing.Verbose())
	s.ExpectSetEventuallyRE("^bar=.*$", "def")
	if s.Error() != nil {
		t.Errorf("unexpected error: %s", s.Error())
	}
	s.ExpectSetEventuallyRE("abc")
	if got, want := s.Error(), "expect_test.go:138: found no match for \"abc\""; got == nil || got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Need to clear the EOF from the previous ExpectSetEventuallyRE call
	buf = []byte{}
	buffer = bytes.NewBuffer(buf)
	s = expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	buffer.WriteString("ooh\n")
	buffer.WriteString("aah\n")
	s.ExpectSetEventuallyRE("zzz")
	if got, want := s.Error(), "expect_test.go:148: found no match for \"zzz\""; got == nil || got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
	s.ExpectEOF()

	buf = []byte{}
	buffer = bytes.NewBuffer(buf)
	s = expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	buffer.WriteString("not expected\n")
	buffer.WriteString("hello world\n")
	buffer.WriteString("this is a test\n")
	matches := s.ExpectSetEventuallyRE("hello (world)", "this (is) (a|b) test")
	if want := [][]string{{"hello world", "world"}, {"this is a test", "is", "a"}}; !reflect.DeepEqual(want, matches) {
		t.Errorf("unexpected result from ExpectSetRE, got %v, want %v", matches, want)
	}
}

func TestRead(t *testing.T) {
	buf := []byte{}
	buffer := bytes.NewBuffer(buf)
	lines := []string{"some words", "bar=baz", "more words"}
	for _, l := range lines {
		buffer.WriteString(l + "\n")
	}
	s := expect.NewSession(nil, bufio.NewReader(buffer), time.Minute)
	for _, l := range lines {
		if got, want := s.ReadLine(), l; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if s.Failed() {
		t.Errorf("unexpected error: %s", s.Error())
	}
	want := ""
	for i := 0; i < 100; i++ {
		m := fmt.Sprintf("%d\n", i)
		buffer.WriteString(m)
		want += m
	}
	got, err := s.ReadAll()
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	s.ExpectEOF()
}
