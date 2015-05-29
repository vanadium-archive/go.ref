// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"encoding/hex"
	"reflect"
	"testing"
	"time"
)

func TestReadBeforeData(t *testing.T) {
	reader := NewTypeReader()
	input := []byte{0, 2, 3, 4, 5}
	data := make([]byte, 5)
	go func() {
		<-time.After(100 * time.Millisecond)
		reader.Add(hex.EncodeToString(input))
	}()
	n, err := reader.Read(data)
	if n != len(data) {
		t.Errorf("Read the wrong number of bytes, wanted:%d, got:%d", len(data), n)
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !reflect.DeepEqual(input, data) {
		t.Errorf("wrong data, want:%x, got:%x", input, data)
	}
}

func TestReadWithPartialData(t *testing.T) {
	reader := NewTypeReader()
	input := []byte{0, 2, 3, 4, 5}
	data := make([]byte, 5)
	reader.Add(hex.EncodeToString(input[:2]))
	go func() {
		<-time.After(300 * time.Millisecond)
		reader.Add(hex.EncodeToString(input[2:]))
	}()
	totalRead := 0
	for {
		n, err := reader.Read(data[totalRead:])
		totalRead += n
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			break
		}
		if totalRead == 5 {
			break
		}
	}
	if totalRead != len(data) {
		t.Errorf("Read the wrong number of bytes, wanted:%d, got:%d", len(data), totalRead)
	}

	if !reflect.DeepEqual(input, data) {
		t.Errorf("wrong data, want:%x, got:%x", input, data)
	}
}
