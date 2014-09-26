package vc

import (
	"testing"
	"time"
)

func TestCancelChannelNil(t *testing.T) {
	var zero time.Time
	if cancel, _ := cancelChannel(zero); cancel != nil {
		t.Errorf("Got %v want nil with deadline %v", cancel, zero)
	}
}

func TestCancelChannel(t *testing.T) {
	deadline := time.Now()
	cancel, _ := cancelChannel(deadline)
	if cancel == nil {
		t.Fatalf("Got nil channel for deadline %v", deadline)
	}
	if _, ok := <-cancel; ok {
		t.Errorf("Expected channel to be closed")
	}
}

func TestCancelChannelQuit(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	cancel, quit := cancelChannel(deadline)
	close(quit)
	if _, ok := <-cancel; ok {
		t.Errorf("Expected channel to be closed")
	}
}
