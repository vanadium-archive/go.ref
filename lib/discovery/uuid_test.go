// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"testing"

	"v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/testdata"
	"github.com/pborman/uuid"
)

func TestServiceUUID(t *testing.T) {
	for _, test := range testdata.InterfaceNameTest {
		if got := uuid.UUID(discovery.NewServiceUUID(test.In)).String(); got != test.Want {
			t.Errorf("ServiceUUID for %q mismatch; got %q, want %q", test.In, got, test.Want)
		}
	}
}

func TestInstanceUUID(t *testing.T) {
	uuids := make(map[string]struct{})
	for x := 0; x < 100; x++ {
		uuid := uuid.UUID(discovery.NewInstanceUUID()).String()
		if _, ok := uuids[uuid]; ok {
			t.Errorf("InstanceUUID returned duplicated UUID %q", uuid)
		}
		uuids[uuid] = struct{}{}
	}
}
