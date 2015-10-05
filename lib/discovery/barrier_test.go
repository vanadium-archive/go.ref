// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"runtime"
	"testing"
)

func TestBarrier(t *testing.T) {
	ch := make(chan struct{})
	done := func() { ch <- struct{}{} }

	// A new barrier; Shouldn't call done.
	br := NewBarrier(done)
	if waitDone(ch) {
		t.Error("unexpected done call")
	}

	// Make sure the barrier works with one sub-closure.
	cb := br.Add()
	cb()
	if !waitDone(ch) {
		t.Error("no expected done call")
	}
	// Try to add a sub-closure, but done has been already called.
	cb = br.Add()
	if cb != nil {
		t.Error("expect nil closure, but got non-nil")
	}

	// Make sure the barrier works with multiple sub-closures.
	br = NewBarrier(done)
	cb1 := br.Add()
	cb2 := br.Add()
	cb1()
	if waitDone(ch) {
		t.Error("unexpected done call")
	}
	cb2()
	if !waitDone(ch) {
		t.Error("no expected done call")
	}
}

func waitDone(ch <-chan struct{}) bool {
	runtime.Gosched()
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
