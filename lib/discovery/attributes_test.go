// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"testing"

	"v.io/v23/discovery"
)

func TestValidateAttributes(t *testing.T) {
	valids := []discovery.Attributes{
		discovery.Attributes{"key": "v"},
		discovery.Attributes{"k_e.y": "v"},
		discovery.Attributes{"k!": "v"},
	}
	for i, attrs := range valids {
		if err := validateAttributes(attrs); err != nil {
			t.Errorf("[%d]: valid attributes got error: %v", i, err)
		}
	}

	invalids := []discovery.Attributes{
		discovery.Attributes{"_key": "v"},
		discovery.Attributes{"k=ey": "v"},
		discovery.Attributes{"key\n": "v"},
	}
	for i, attrs := range invalids {
		if err := validateAttributes(attrs); err == nil {
			t.Errorf("[%d]: invalid attributes didn't get error", i)
		}
	}
}
