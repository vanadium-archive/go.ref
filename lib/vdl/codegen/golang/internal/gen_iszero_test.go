// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import "testing"

type hasIsZero interface {
	IsZero() bool
}

func TestIsZero(t *testing.T) {
	tests := []struct {
		Value  hasIsZero
		IsZero bool
	}{
		{Value: ZA, IsZero: true},
		{Value: NZA, IsZero: false},
		{Value: &ZB, IsZero: true},
		{Value: &NZB, IsZero: false},
		{Value: &ZC, IsZero: true},
		{Value: &NZC, IsZero: false},
		{Value: ZD, IsZero: true},
		{Value: NZD, IsZero: false},
		{Value: ZE, IsZero: true},
		{Value: NZE, IsZero: false},
		{Value: ZF, IsZero: true},
		{Value: NZF, IsZero: false},
		{Value: ZG, IsZero: true},
		{Value: NZG, IsZero: false},
		{Value: ZH, IsZero: true},
		{Value: NZH, IsZero: false},
		{Value: ZI, IsZero: true},
		{Value: NZI1, IsZero: false},
		{Value: NZI2, IsZero: false},
	}
	for _, test := range tests {
		if got, want := test.Value.IsZero(), test.IsZero; got != want {
			t.Errorf("for value %v: got %v, want %v", test.Value, got, want)
		}
	}
}
