package rpc

import (
	"testing"
	"time"
)

func TestTimer(t *testing.T) {
	test := newTimer(time.Millisecond)
	if _, ok := <-test.C; ok {
		t.Errorf("Expected the channel to be closed.")
	}

	// Test resetting.
	test = newTimer(time.Hour)
	if reset := test.Reset(time.Millisecond); !reset {
		t.Errorf("Expected to successfully reset.")
	}
	if _, ok := <-test.C; ok {
		t.Errorf("Expected the channel to be closed.")
	}

	// Test stop.
	test = newTimer(100 * time.Millisecond)
	test.Stop()
	select {
	case <-test.C:
		t.Errorf("the test timer should have been stopped.")
	case <-time.After(200 * time.Millisecond):
	}
}
