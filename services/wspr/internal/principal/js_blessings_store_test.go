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

	h := s.GetOrAddHandle(b)
	if got := s.GetBlessings(h); !reflect.DeepEqual(got, b) {
		t.Fatalf("Get after adding: got: %v, want: %v", got, b)
	}

	hGetOrAdd := s.GetOrAddHandle(b)
	if h != hGetOrAdd {
		t.Fatalf("Expected same handle from get or add. got: %v, want: %v", hGetOrAdd, h)
	}
	if len(s.store) != 1 {
		t.Fatalf("Expected single entry to exist")
	}

	b2 := blessSelf(testutil.NewPrincipal(), "secondBlessing")
	hNewFromGetOrAdd := s.GetOrAddHandle(b2)
	if hNewFromGetOrAdd == h {
		t.Fatalf("Expected to get new handle on new name. got: %v, want: %v", hGetOrAdd, h)
	}

	s.RemoveReference(h)
	if got := s.GetBlessings(h); !reflect.DeepEqual(got, b) {
		t.Fatalf("Expected to still be able to find after first remove: got: %v, want %v", got, b)
	}

	s.RemoveReference(h)
	if got := s.GetBlessings(h); !got.IsZero() {
		t.Fatalf("Get after removing: got: %v, want nil", got)
	}

	if got := s.GetBlessings(hNewFromGetOrAdd); !reflect.DeepEqual(got, b2) {
		t.Fatalf("Expected to still be able to get second blessing: got: %v, want %v", got, b2)
	}

	s.RemoveReference(hNewFromGetOrAdd)
	if got := s.GetBlessings(h); !got.IsZero() {
		t.Fatalf("Get after removing: got: %v, want nil", got)
	}
}
