package core

import "testing"

func TestCheckArgs(t *testing.T) {
	if got, want := checkArgs([]string{}, 1, "<a>"), `wrong # args (got 0, expected 1) expected: "<a>" got: []`; got == nil || got.Error() != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := checkArgs([]string{}, -1, "<a>"), `wrong # args (got 0, expected >=1) expected: "<a>" got: []`; got == nil || got.Error() != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := checkArgs([]string{"a"}, 1, ""); got != nil {
		t.Errorf("unexpected error: %s", got)
	}
	if got := checkArgs([]string{"a"}, -1, ""); got != nil {
		t.Errorf("unexpected error: %s", got)
	}
	if got := checkArgs([]string{"a", "b"}, -1, ""); got != nil {
		t.Errorf("unexpected error: %s", got)
	}
}
