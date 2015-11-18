// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"testing"

	"github.com/pborman/uuid"
	"v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/testdata"
)

func TestServiceUUID(t *testing.T) {
	for _, test := range testdata.InterfaceNameTest {
		if got := uuid.UUID(discovery.NewServiceUUID(test.In)).String(); got != test.Want {
			t.Errorf("ServiceUUID for %q mismatch; got %q, want %q", test.In, got, test.Want)
		}
	}
}
