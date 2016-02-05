// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"unicode/utf8"

	"v.io/v23/discovery"
)

func TestCopyService(t *testing.T) {
	rand := rand.New(rand.NewSource(0))
	gen := func(i interface{}) {
		v := reflect.Indirect(reflect.ValueOf(i))
		r, ok := quick.Value(v.Type(), rand)
		if !ok {
			t.Fatalf("failed to populate value for %v", i)
		}
		v.Set(r)
	}

	for i := 0; i < 10; i++ {
		var org discovery.Service
		gen(&org.InstanceId)
		gen(&org.Addrs)
		copied := copyService(&org)
		if !reflect.DeepEqual(org, copied) {
			t.Errorf("copied service is different from the original: %v, %v", org, copied)
		}
	}
}

func TestInstanceId(t *testing.T) {
	instanceIds := make(map[string]struct{})
	for x := 0; x < 100; x++ {
		id := newInstanceId()

		if !utf8.ValidString(id) {
			t.Errorf("newInstanceId returned invalid utf-8 string %x", id)
		}
		if _, ok := instanceIds[id]; ok {
			t.Errorf("newInstanceId returned duplicated id %x", id)
		}
		instanceIds[id] = struct{}{}
	}
}
