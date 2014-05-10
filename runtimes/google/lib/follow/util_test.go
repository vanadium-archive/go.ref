package follow

import (
	"errors"
	"strings"
	"testing"
)

// TestComposeErrors tests composeErrors() with combinations of nil and non-nil inputs
func TestComposeErrors(t *testing.T) {
	// no errors compose to a nil error.
	if err := composeErrors(); err != nil {
		t.Fatalf("Expected composeErrors() to return nil, not: %v", err)
	}
	// a single nil error composes to a nil error.
	if err := composeErrors(nil); err != nil {
		t.Fatalf("Expected composeErrors(nil) to return nil, not: %v", err)
	}
	// multiple nil errors compose to a nil error.
	if err := composeErrors(nil, nil); err != nil {
		t.Fatalf("Expected composeErrors(nil, nil) to return nil, not: %v", err)
	}
	// a single non-nil error composes to itself.
	err1 := errors.New("err1")
	if err := composeErrors(err1); err != err1 {
		t.Fatalf("Expected composeErrors(err) to return %v, not: %v", err1, err)
	}
	if err := composeErrors(nil, err1); err != err1 {
		t.Fatalf("Expected composeErrors(nil, err) to return %v, not: %v", err1, err)
	}
	if err := composeErrors(err1, nil); err != err1 {
		t.Fatalf("Expected composeErrors(err, nil) to return %v, not: %v", err1, err)
	}
	// multiple non-nil errors compose to composite error.
	err2 := errors.New("err2")
	err := composeErrors(err1, err2)
	if err == nil {
		t.Fatal("Expected composeErrors(err1, err2) to be non-nil")
	}
	if !strings.Contains(err.Error(), err1.Error()) {
		t.Fatalf("Expected composeErrors(err1, err2) to contain %v, but was: %v", err1, err)
	}
	if !strings.Contains(err.Error(), err2.Error()) {
		t.Fatalf("Expected composeErrors(err1, err2) to contain %v, but was: %v", err2, err)
	}
	// multiple non-nil errors compose to composite error without nil errors.
	err = composeErrors(err1, err2, nil)
	if err == nil {
		t.Fatal("Expected composeErrors(err1, err2, nil) to be non-nil")
	}
	if !strings.Contains(err.Error(), err1.Error()) {
		t.Fatalf("Expected composeErrors(err1, err2, nil) to contain %v, but was: %v", err1, err)
	}
	if !strings.Contains(err.Error(), err2.Error()) {
		t.Fatalf("Expected composeErrors(err1, err2, nil) to contain %v, but was: %v", err2, err)
	}
}
