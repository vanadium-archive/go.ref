// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"testing"

	"v.io/v23/discovery"
	"v.io/x/ref/test"
)

func TestQuery(t *testing.T) {
	ctx, shutdown := test.TestContext()
	defer shutdown()

	services := []discovery.Service{
		{
			InstanceId:    "1",
			InterfaceName: "v.io/v23/a",
			Attrs:         discovery.Attributes{"a1": "v1", "a2": "v2"},
			Addrs:         []string{"/h1:123/x"},
		},
		{
			InstanceId:    "2",
			InterfaceName: "v.io/v23/a",
			Attrs:         discovery.Attributes{"a1": "v2"},
			Addrs:         []string{"/h2:123/x"},
		},
		{
			InstanceId:    "3",
			InterfaceName: "v.io/v23/b",
			Attrs:         discovery.Attributes{"a1": "v1"},
			Addrs:         []string{"/h3:123/y"},
		},
		{
			InstanceId:    "4",
			InterfaceName: "v.io/v23/b/c",
			Addrs:         []string{"/h4:123/y"},
		},
	}

	tests := []struct {
		query   string
		target  Uuid
		matches []bool
	}{
		{"", nil, []bool{true, true, true, true}},
		{`v.InterfaceName="v.io/v23/a"`, NewServiceUUID("v.io/v23/a"), []bool{true, true, false, false}},
		{`v.InterfaceName="v.io/v23/c"`, NewServiceUUID("v.io/v23/c"), []bool{false, false, false, false}},
		{`v.InterfaceName="v.io/v23/a" AND v.Attrs["a1"]="v1"`, NewServiceUUID("v.io/v23/a"), []bool{true, false, false, false}},
		{`v.InterfaceName="v.io/v23/a" AND (v.Attrs["a1"]="v2" OR v.Attrs["a2"] = "v2")`, NewServiceUUID("v.io/v23/a"), []bool{true, true, false, false}},
		{`v.InterfaceName="v.io/v23/a" OR v.InterfaceName="v.io/v23/b"`, nil, []bool{true, true, true, false}},
		{`v.InterfaceName<>"v.io/v23/a"`, nil, []bool{false, false, true, true}},
		{`v.InterfaceName LIKE "v.io/v23/b%"`, nil, []bool{false, false, true, true}},
		{`v.InterfaceName="v.io/v23/a" OR v.InterfaceName LIKE "v.io/v23/b%"`, nil, []bool{true, true, true, true}},
		{`v.Attrs["a1"]="v1"`, nil, []bool{true, false, true, false}},
		{`k = "4"`, nil, []bool{false, false, false, true}},
	}

	for i, test := range tests {
		m, err := newMatcher(ctx, test.query)
		if err != nil {
			t.Errorf("query[%d]: newMatcher failed: %v", i, err)
			continue
		}

		if target := m.targetServiceUuid(); !bytes.Equal(target, test.target) {
			t.Errorf("query[%d]: got target %v; but wanted %v", i, target, test.target)
		}

		for j, service := range services {
			if matched := m.match(&Advertisement{Service: service}); matched != test.matches[j] {
				t.Errorf("query[%d]: match returned %t for service[%d]; but wanted %t", i, matched, j, test.matches[j])
			}
		}
	}
}

func TestQueryError(t *testing.T) {
	ctx, shutdown := test.TestContext()
	defer shutdown()

	tests := []string{
		`v..InterfaceName="v.io/v23/a"`,
		`v.InterfaceName="v.io/v23/a" AND AND v.Attrs["a1"]="v1"`,
		`v.Attrs["a1"]=`,
	}

	for i, test := range tests {
		if _, err := newMatcher(ctx, test); err == nil {
			t.Errorf("query[%d]: newMatcher not failed", i)
		}
	}
}
