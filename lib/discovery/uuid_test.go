// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"testing"

	"v.io/x/ref/lib/discovery"
)

func TestServiceUUID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"v.io", "2101363c-688d-548a-a600-34d506e1aad0"},
		{"v.io/v23/abc", "6726c4e5-b6eb-5547-9228-b2913f4fad52"},
		{"v.io/v23/abc/xyz", "be8a57d7-931d-5ee4-9243-0bebde0029a5"},
	}

	for _, test := range tests {
		if got := discovery.NewServiceUUID(test.in).String(); got != test.want {
			t.Errorf("ServiceUUID for %q mismatch; got %q, want %q", test.in, got, test.want)
		}
	}
}

func TestInstanceUUID(t *testing.T) {
	uuids := make(map[string]struct{})
	for x := 0; x < 100; x++ {
		uuid := discovery.NewInstanceUUID().String()
		if _, ok := uuids[uuid]; ok {
			t.Errorf("InstanceUUID returned duplicated UUID %q", uuid)
		}
		uuids[uuid] = struct{}{}
	}
}
