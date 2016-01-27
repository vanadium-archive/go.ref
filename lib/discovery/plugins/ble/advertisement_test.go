// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"reflect"
	"testing"

	"github.com/pborman/uuid"

	"v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/plugins/ble/testdata"
)

func TestConvertingBackAndForth(t *testing.T) {
	for i, test := range testdata.ConversionTestData {
		adv := test.Advertisement
		serviceUuid := uuid.UUID(discovery.NewServiceUUID(adv.Service.InterfaceName))
		bleAdv := newBleAdvertisment(serviceUuid, adv)
		if !uuid.Equal(bleAdv.serviceUuid, serviceUuid) {
			t.Errorf("[%d]: wanted %v, but got %v", i, serviceUuid, bleAdv.serviceUuid)
		}
		if !reflect.DeepEqual(bleAdv.attrs, test.BleAdvertisement) {
			t.Errorf("[%d]: wanted %v, but got %v", i, test.BleAdvertisement, bleAdv.attrs)
		}

		out, err := bleAdv.toDiscoveryAdvertisement()
		if err != nil {
			t.Errorf("[%d]: unexpected error: %v", i, err)
		}
		if !reflect.DeepEqual(&adv, out) {
			t.Errorf("[%d]: wanted %v, but got %v", i, adv, out)
		}
	}
}
