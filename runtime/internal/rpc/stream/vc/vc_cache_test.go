// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"testing"

	"v.io/v23/naming"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/test/testutil"
)

func TestInsertDelete(t *testing.T) {
	cache := NewVCCache()
	ep, err := inaming.NewEndpoint("foo:8888")
	if err != nil {
		t.Fatal(err)
	}
	p := testutil.NewPrincipal("test")
	vc := &VC{remoteEP: ep, localPrincipal: p}
	otherEP, err := inaming.NewEndpoint("foo:8888")
	if err != nil {
		t.Fatal(err)
	}
	otherP := testutil.NewPrincipal("test")
	otherVC := &VC{remoteEP: otherEP, localPrincipal: otherP}

	cache.Insert(vc)
	cache.Insert(otherVC)
	cache.Delete(vc)
	if got, want := cache.Close(), []*VC{otherVC}; !vcsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInsertClose(t *testing.T) {
	cache := NewVCCache()
	ep, err := inaming.NewEndpoint("foo:8888")
	if err != nil {
		t.Fatal(err)
	}
	p := testutil.NewPrincipal("test")
	vc := &VC{remoteEP: ep, localPrincipal: p}

	if err := cache.Insert(vc); err != nil {
		t.Errorf("the cache is not closed yet")
	}
	if got, want := cache.Close(), []*VC{vc}; !vcsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if err := cache.Insert(vc); err == nil {
		t.Errorf("the cache has been closed")
	}
}

func TestReservedFind(t *testing.T) {
	cache := NewVCCache()
	ep, err := inaming.NewEndpoint("foo:8888")
	if err != nil {
		t.Fatal(err)
	}
	p := testutil.NewPrincipal("test")
	vc := &VC{remoteEP: ep, localPrincipal: p}
	cache.Insert(vc)

	// We should be able to find the vc in the cache.
	if got, err := cache.ReservedFind(ep, p); err != nil || got != vc {
		t.Errorf("got %v, want %v, err: %v", got, vc, err)
	}

	// If we change the endpoint or the principal, we should get nothing.
	otherEP, err := inaming.NewEndpoint("bar: 7777")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := cache.ReservedFind(otherEP, p); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}
	if got, err := cache.ReservedFind(ep, testutil.NewPrincipal("wrong")); err != nil || got != nil {
		t.Errorf("got %v, want <nil>, err: %v", got, err)
	}

	// A subsequent ReservedFind call that matches a previous failed ReservedFind
	// should block until a matching Unreserve call is made.
	ch := make(chan *VC, 1)
	go func(ch chan *VC) {
		vc, err := cache.ReservedFind(otherEP, p)
		if err != nil {
			t.Fatal(err)
		}
		ch <- vc
	}(ch)

	// We insert the otherEP into the cache.
	otherVC := &VC{remoteEP: otherEP, localPrincipal: p}
	cache.Insert(otherVC)
	cache.Unreserve(otherEP, p)

	// Now the cache.BlcokingFind should have returned the correct otherVC.
	if cachedVC := <-ch; cachedVC != otherVC {
		t.Errorf("got %v, want %v", cachedVC, otherVC)
	}

	// If we add an endpoint with a non-zero routingId and search for another
	// endpoint with the same routingID, we should get the first routingID.
	ridep, err := inaming.NewEndpoint("oink:8888")
	if err != nil {
		t.Fatal(err)
	}
	ridep.RID = naming.FixedRoutingID(0x1111)
	vc = &VC{remoteEP: ridep, localPrincipal: p}
	cache.Insert(vc)
	otherEP, err = inaming.NewEndpoint("moo:7777")
	if err != nil {
		t.Fatal(err)
	}
	otherEP.RID = ridep.RID
	if got, err := cache.ReservedFind(otherEP, p); err != nil || got != vc {
		t.Errorf("got %v, want %v, err: %v", got, vc, err)
	}
}

func vcsEqual(a, b []*VC) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[*VC]int)
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		m[v]--
	}
	for _, i := range m {
		if i != 0 {
			return false
		}
	}
	return true
}
