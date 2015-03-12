package testutil

import (
	"regexp"
	"testing"
)

func TestFormatLogline(t *testing.T) {
	depth, want := DepthToExternalCaller(), 2
	if depth != want {
		t.Errorf("got %v, want %v", depth, want)
	}
	{
		line, want := FormatLogLine(depth, "test"), "testing.go:.*"
		if ok, err := regexp.MatchString(want, line); !ok || err != nil {
			t.Errorf("got %v, want %v", line, want)
		}
	}
}
