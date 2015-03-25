// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"reflect"
	"testing"
)

func TestIDProvider(t *testing.T) {
	idp := NewIDProvider("foo")
	p := NewPrincipal()
	if err := idp.Bless(p, "bar"); err != nil {
		t.Fatal(err)
	}
	if err := p.Roots().Recognized(idp.PublicKey(), "foo"); err != nil {
		t.Error(err)
	}
	if err := p.Roots().Recognized(idp.PublicKey(), "foo/bar"); err != nil {
		t.Error(err)
	}
	def := p.BlessingStore().Default()
	peers := p.BlessingStore().ForPeer("anyone_else")
	if def.IsZero() {
		t.Errorf("BlessingStore should have a default blessing")
	}
	if !reflect.DeepEqual(peers, def) {
		t.Errorf("ForPeer(...) returned %v, want %v", peers, def)
	}
	// TODO(ashankar): Implement a security.Call and test the string
	// values as well.
}
