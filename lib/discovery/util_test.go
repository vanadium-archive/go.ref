// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"unicode/utf8"

	"v.io/v23/discovery"
)

func TestCopyService(t *testing.T) {
	rand := rand.New(rand.NewSource(0))
	for i := 0; i < 10; i++ {
		v, ok := quick.Value(reflect.TypeOf(discovery.Service{}), rand)
		if !ok {
			t.Fatal("failed to populate service")
		}
		org := v.Interface().(discovery.Service)
		copied := copyService(&org)
		if !reflect.DeepEqual(org, copied) {
			t.Errorf("copied service is different from the original: %v, %v", org, copied)
		}
	}
}

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

func TestHashAdvertisement(t *testing.T) {
	a1 := Advertisement{
		Service: discovery.Service{
			InstanceId:    "123",
			InterfaceName: "v.io/x",
			Attrs: map[string]string{
				"k1": "v1",
				"k2": "v2",
			},
			Addrs: []string{
				"/@6@wsh@foo.com:1234@@/x",
			},
		}}

	// Shouldn't be changed by hashing.
	a2 := a1
	hashAdvertisement(&a2)
	a2.Hash = nil
	if !reflect.DeepEqual(a1, a2) {
		t.Errorf("shouldn't be changed by hash: %v, %v", a1, a2)
	}

	// Should have the same hash for the same advertisements.
	hashAdvertisement(&a1)
	hashAdvertisement(&a2)
	if !bytes.Equal(a1.Hash, a2.Hash) {
		t.Errorf("expected same hash, but got different: %v, %v", a1.Hash, a2.Hash)
	}

	// Should be idempotent.
	hashAdvertisement(&a2)
	if !bytes.Equal(a1.Hash, a2.Hash) {
		t.Errorf("expected same hash, but got different: %v, %v", a1.Hash, a2.Hash)
	}

	a2.Service.InstanceId = "456"
	hashAdvertisement(&a2)
	if bytes.Equal(a1.Hash, a2.Hash) {
		t.Errorf("expected different hashes, but got same: %v, %v", a1.Hash, a2.Hash)
	}

	// Should distinguish between {"", "x"} and {"x", ""}.
	a2 = a1
	a2.Service.InstanceName = a1.Service.InterfaceName
	a2.Service.InterfaceName = ""
	hashAdvertisement(&a2)
	if bytes.Equal(a1.Hash, a2.Hash) {
		t.Errorf("expected different hashes, but got same: %v, %v", a1.Hash, a2.Hash)
	}

	a2 = a1
	a2.Service.Attrs = nil
	a2.Service.Attachments = make(discovery.Attachments)
	for k, v := range a1.Service.Attrs {
		a2.Service.Attachments[k] = []byte(v)
	}
	hashAdvertisement(&a2)
	if bytes.Equal(a1.Hash, a2.Hash) {
		t.Errorf("expected different hashes, but got same: %v, %v", a1.Hash, a2.Hash)
	}

	// Shouldn't distinguish map order.
	a2 = a1
	a2.Service.Attrs = make(map[string]string)
	var keys []string
	for k, _ := range a1.Service.Attrs {
		keys = append(keys, k)
	}
	for i := len(keys) - 1; i >= 0; i-- {
		a2.Service.Attrs[keys[i]] = a1.Service.Attrs[keys[i]]
	}
	hashAdvertisement(&a2)
	if !bytes.Equal(a1.Hash, a2.Hash) {
		t.Errorf("expected same hash, but got different: %v, %v", a1.Hash, a2.Hash)
	}

	// Shouldn't distinguish between nil and empty.
	a2 = a1
	a2.Service.Attachments = make(discovery.Attachments)
	hashAdvertisement(&a2)
	if !bytes.Equal(a1.Hash, a2.Hash) {
		t.Errorf("expected same hash, but got different: %v, %v", a1.Hash, a2.Hash)
	}
}

func TestHashAdvertisementCoverage(t *testing.T) {
	rand := rand.New(rand.NewSource(0))
	gen := func(v reflect.Value) {
		for {
			r, ok := quick.Value(v.Type(), rand)
			if !ok {
				t.Fatalf("failed to populate value for %v", v)
			}
			switch v.Kind() {
			case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
				if r.Len() == 0 {
					continue
				}
			}
			if reflect.DeepEqual(v, r) {
				continue
			}
			v.Set(r)
			return
		}
	}

	// Ensure that every single field of advertisement is hashed.
	ad := Advertisement{}
	hashAdvertisement(&ad)

	for ty, i := reflect.TypeOf(ad.Service), 0; i < ty.NumField(); i++ {
		oldAd := ad

		field := reflect.ValueOf(&ad.Service).Elem().Field(i)
		gen(field)
		hashAdvertisement(&ad)

		if bytes.Equal(oldAd.Hash, ad.Hash) {
			t.Errorf("Service.%s: expected different hashes, but got same: %v, %v", field.Type().Name(), oldAd.Hash, ad.Hash)
		}
	}

	for ty, i := reflect.TypeOf(ad), 0; i < ty.NumField(); i++ {
		oldAd := ad

		field := reflect.ValueOf(&ad.Service).Elem().Field(i)
		switch field.Type().Name() {
		case "Service", "Hash":
			continue
		}

		gen(field)
		hashAdvertisement(&ad)

		if bytes.Equal(oldAd.Hash, ad.Hash) {
			t.Errorf("Advertisement.%s: expected different hashes, but got same: %v, %v", field.Type().Name(), oldAd.Hash, ad.Hash)
		}
	}
}
