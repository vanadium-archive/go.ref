package expect_test

import (
	"bufio"
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"veyron/lib/expect"
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
}
