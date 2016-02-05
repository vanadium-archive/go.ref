// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"testing"

	"v.io/v23/discovery"
	"v.io/x/ref/test"
)

func TestQuery(t *testing.T) {
	ctx, shutdown := test.TestContext()
	defer shutdown()

	services := []discovery.Service{
		{
			InstanceId: "1",
			Addrs:      []string{"/h1:123/x"},
		},
		{
			InstanceId: "2",
			Addrs:      []string{"/h1:456/x"},
		},
		{
			InstanceId: "3",
			Addrs:      []string{"/h3:123/y"},
		},
	}

	tests := []struct {
		query   string
		target  string
		matches []bool
	}{
		{"", "", []bool{true, true, true}},
		{`k = "1" OR k = "2"`, "", []bool{true, true, false}},
		{`k = "3"`, "3", []bool{false, false, true}},
		{`v.Addrs[0] LIKE "/h1:%"`, "", []bool{true, true, false}},
	}

	for i, test := range tests {
		m, target, err := newMatcher(ctx, test.query)
		if err != nil {
			t.Errorf("query[%d]: newMatcher failed: %v", i, err)
			continue
		}

		if target != test.target {
			t.Errorf("query[%d]: got target %v; but wanted %v", i, target, test.target)
		}

		for j, service := range services {
			if matched := m.match(&service); matched != test.matches[j] {
				t.Errorf("query[%d]: match returned %t for service[%d]; but wanted %t", i, matched, j, test.matches[j])
			}
		}
	}
}
