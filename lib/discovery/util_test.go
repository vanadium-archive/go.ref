// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"testing"
	"unicode/utf8"
)

func TestInstanceId(t *testing.T) {
	instanceIds := make(map[string]struct{})
	for x := 0; x < 100; x++ {
		id, err := newInstanceId()
		if err != nil {
			t.Error(err)
			continue
		}

		if !utf8.ValidString(id) {
			t.Errorf("newInstanceId returned invalid utf-8 string %x", id)
		}

		if _, ok := instanceIds[id]; ok {
			t.Errorf("newInstanceId returned duplicated id %x", id)
		}
		instanceIds[id] = struct{}{}
	}
}
