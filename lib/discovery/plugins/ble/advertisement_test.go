// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"github.com/pborman/uuid"
	"reflect"
	"testing"
	vdiscovery "v.io/v23/discovery"
	"v.io/x/ref/lib/discovery"
)

func TestConvertingBackAndForth(t *testing.T) {
	v23Adv := discovery.Advertisement{
		Service: vdiscovery.Service{
			Addrs:        []string{"localhost:1000", "example.com:540"},
			InstanceUuid: []byte(uuid.NewUUID()),
			Attrs: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		ServiceUuid: uuid.NewUUID(),
	}

	adv := newAdvertisment(v23Adv)
	out, err := adv.toDiscoveryAdvertisement()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(&v23Adv, out) {
		t.Errorf("input does not equal output: %v, %v", v23Adv, out)
	}
}
