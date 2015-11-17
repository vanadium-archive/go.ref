// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"bytes"
	"reflect"
	"testing"

	"v.io/x/ref/lib/discovery/plugins/ble/testdata"
)

func TestConvertingBackAndForth(t *testing.T) {
	for _, test := range testdata.ConversionTestData {
		v23Adv := test.VAdvertisement
		adv := newAdvertisment(v23Adv)
		if !bytes.Equal(adv.serviceUuid, v23Adv.ServiceUuid) {
			t.Errorf("wanted: %v, got %v", adv.serviceUuid, v23Adv.ServiceUuid)
		}
		if !reflect.DeepEqual(adv.attrs, test.BleAdvertisement) {
			t.Errorf("wanted: %v, got %v", test.BleAdvertisement, adv.attrs)
		}

		out, err := adv.toDiscoveryAdvertisement()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(&v23Adv, out) {
			t.Errorf("input does not equal output: %v, %v", v23Adv, out)
		}
	}
}
