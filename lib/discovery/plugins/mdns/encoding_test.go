// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mdns

import (
	"crypto/rand"
	"encoding/base64"
	"reflect"
	"sort"
	"testing"
)

func TestEncodeInstanceUuid(t *testing.T) {
	tests := [][]byte{
		randInstanceUuid(1),
		randInstanceUuid(10),
		randInstanceUuid(16),
		randInstanceUuid(32),
	}

	for i, test := range tests {
		encoded := encodeInstanceUuid(test)
		instanceUuid, err := decodeInstanceUuid(encoded)
		if err != nil {
			t.Errorf("[%d]: decodeInstanceUuid failed: %v", i, err)
			continue
		}
		if !reflect.DeepEqual(instanceUuid, test) {
			t.Errorf("[%d]: decoded to %v, but want %v", i, instanceUuid, test)
		}
	}
}

func randInstanceUuid(n int) []byte {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return b
}

func TestSplitLargeTxt(t *testing.T) {
	tests := [][]string{
		[]string{randTxt(maxTxtRecordLen / 2)},
		[]string{randTxt(maxTxtRecordLen / 2), randTxt(maxTxtRecordLen / 3)},
		[]string{randTxt(maxTxtRecordLen * 2)},
		[]string{randTxt(maxTxtRecordLen * 2), randTxt(maxTxtRecordLen * 3)},
		[]string{randTxt(maxTxtRecordLen / 2), randTxt(maxTxtRecordLen * 3), randTxt(maxTxtRecordLen * 2), randTxt(maxTxtRecordLen / 3)},
	}

	for i, test := range tests {
		splitted, err := maybeSplitLargeTXT(test)
		if err != nil {
			t.Errorf("[%d]: encodeLargeTxt failed: %v", i, err)
			continue
		}
		for _, v := range splitted {
			if len(v) > maxTxtRecordLen {
				t.Errorf("[%d]: too large encoded txt %d - %v", i, len(v), v)
			}
		}

		txt, err := maybeJoinLargeTXT(splitted)
		if err != nil {
			t.Errorf("[%d]: decodeLargeTxt failed: %v", i, err)
			continue
		}

		sort.Strings(txt)
		sort.Strings(test)
		if !reflect.DeepEqual(txt, test) {
			t.Errorf("[%d]: decoded to %#v, but want %#v", i, txt, test)
		}
	}
}

func randTxt(n int) string {
	b := make([]byte, int((n*3+3)/4))
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return base64.RawStdEncoding.EncodeToString(b)[:n]
}
