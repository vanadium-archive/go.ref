// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package principal

import (
	"reflect"
	"testing"

	"v.io/x/ref/test/testutil"
)

func TestJSBlessingStore(t *testing.T) {
	s := NewJSBlessingsHandles()
	b := blessSelf(testutil.NewPrincipal(), "irrelevant")

	h := s.Add(b)
	if got := s.Get(h); !reflect.DeepEqual(got, b) {
		t.Fatalf("Get after adding: got: %v, want: %v", got, b)
	}

	s.Remove(h)
	if got := s.Get(h); !got.IsZero() {
		t.Fatalf("Get after removing: got: %v, want nil", got)
	}
}
