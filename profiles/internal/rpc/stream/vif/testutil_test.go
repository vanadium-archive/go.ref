// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"fmt"
	"time"
)

// WaitForNotifications waits till all notifications in 'wants' have been received.
func WaitForNotifications(notify <-chan interface{}, wants ...interface{}) error {
	expected := make(map[interface{}]struct{})
	for _, w := range wants {
		expected[w] = struct{}{}
	}
	for len(expected) > 0 {
		n := <-notify
		if _, exists := expected[n]; !exists {
			return fmt.Errorf("unexpected notification %v", n)
		}
		delete(expected, n)
	}
	return nil
}

// WaitWithTimeout returns error if any notification has been received before
// the timeout expires.
func WaitWithTimeout(notify <-chan interface{}, timeout time.Duration) error {
	timer := time.After(timeout)
	for {
		select {
		case n := <-notify:
			return fmt.Errorf("unexpected notification %v", n)
		case <-timer:
			return nil
		}
	}
}
