// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"fmt"
	"reflect"
	"time"
)

// WaitForNotifications waits till all notifications in 'wants' have been received,
// or the timeout expires.
func WaitForNotifications(notify <-chan interface{}, timeout time.Duration, wants ...interface{}) error {
	timer := make(<-chan time.Time)
	if timeout > 0 {
		timer = time.After(timeout)
	}

	received := make(map[interface{}]struct{})
	want := make(map[interface{}]struct{})
	for _, w := range wants {
		want[w] = struct{}{}
	}
	for {
		select {
		case n := <-notify:
			received[n] = struct{}{}
			if _, exists := want[n]; !exists {
				return fmt.Errorf("unexpected notification %v", n)
			}
			if reflect.DeepEqual(received, want) {
				return nil
			}
		case <-timer:
			if len(wants) == 0 {
				// No notification wanted.
				return nil
			}
			return fmt.Errorf("timeout after receiving %v", reflect.ValueOf(received).MapKeys())
		}
	}
}
