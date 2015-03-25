// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"time"
)

// timer is a replacement for time.Timer, the only difference is that
// its channel is type chan struct{} and it will be closed when the timer expires,
// which we need in some places.
type timer struct {
	base *time.Timer
	C    <-chan struct{}
}

func newTimer(d time.Duration) *timer {
	c := make(chan struct{}, 0)
	base := time.AfterFunc(d, func() {
		close(c)
	})
	return &timer{
		base: base,
		C:    c,
	}
}

func (t *timer) Stop() bool {
	return t.base.Stop()
}

func (t *timer) Reset(d time.Duration) bool {
	return t.base.Reset(d)
}
