// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package message

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"v.io/v23/naming"
	"v.io/x/ref/profiles/internal/lib/iobuf"
	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/profiles/internal/rpc/stream/crypto"
	iversion "v.io/x/ref/profiles/internal/rpc/version"
)

// testControlCipher is a super-simple cipher that xor's each byte of the
// payload with 0xaa.
type testControlCipher struct{}

const testMACSize = 4

func (*testControlCipher) MACSize() int {
	return testMACSize
}

func testMAC(data []byte) []byte {
	var h uint32
	for _, b := range data {
		h = (h << 1) ^ uint32(b)
	}
	var hash [4]byte
	binary.BigEndian.PutUint32(hash[:], h)
	return hash[:]
}

func (c *testControlCipher) Decrypt(data []byte) {
	for i, _ := range data {
		data[i] ^= 0xaa
	}
}

func (c *testControlCipher) Encrypt(data []byte) {
	for i, _ := range data {
		data[i] ^= 0xaa
	}
}

func (c *testControlCipher) Open(data []byte) bool {
	mac := testMAC(data[:len(data)-testMACSize])
	if bytes.Compare(mac, data[len(data)-testMACSize:]) != 0 {
		return false
	}
	c.Decrypt(data[:len(data)-testMACSize])
	return true
}

func (c *testControlCipher) Seal(data []byte) error {
	c.Encrypt(data[:len(data)-testMACSize])
	mac := testMAC(data[:len(data)-testMACSize])
	copy(data[len(data)-testMACSize:], mac)
	return nil
}

func TestControl(t *testing.T) {
	counters := NewCounters()
	counters.Add(12, 13, 10240)
	tests := []Control{
		&CloseVC{VCI: 1},
		&CloseVC{VCI: 2, Error: "some error"},

		&SetupVC{
			VCI: 1,
			LocalEndpoint: &inaming.Endpoint{
				Protocol: "tcp",
				Address:  "batman.com:1990",
				RID:      naming.FixedRoutingID(0xba7),
			},
			RemoteEndpoint: &inaming.Endpoint{
				Protocol: "tcp",
				Address:  "bugsbunny.com:1940",
				RID:      naming.FixedRoutingID(0xbb),
			},
			Counters: counters,
			Setup: Setup{
				Versions: iversion.Range{Min: 34, Max: 56},
				Options: []SetupOption{
					&NaclBox{PublicKey: crypto.BoxKey{'h', 'e', 'l', 'l', 'o', 'w', 'o', 'r', 'l', 'd'}},
					&NaclBox{PublicKey: crypto.BoxKey{7, 67, 31}},
				},
			},
		},
		// SetupVC without endpoints
		&SetupVC{
			VCI:      1,
			Counters: counters,
			Setup: Setup{
				Versions: iversion.Range{Min: 34, Max: 56},
				Options: []SetupOption{
					&NaclBox{PublicKey: crypto.BoxKey{'h', 'e', 'l', 'l', 'o', 'w', 'o', 'r', 'l', 'd'}},
					&NaclBox{PublicKey: crypto.BoxKey{7, 67, 31}},
				},
			},
		},

		&AddReceiveBuffers{},
		&AddReceiveBuffers{Counters: counters},

		&OpenFlow{VCI: 1, Flow: 10, InitialCounters: 1 << 24},

		&Setup{
			Versions: iversion.Range{Min: 21, Max: 71},
			Options: []SetupOption{
				&NaclBox{PublicKey: crypto.BoxKey{'h', 'e', 'l', 'l', 'o', 'w', 'o', 'r', 'l', 'd'}},
				&NaclBox{PublicKey: crypto.BoxKey{7, 67, 31}},
			},
		},

		&SetupStream{Data: []byte("HelloWorld")},
	}

	var c testControlCipher
	pool := iobuf.NewPool(0)
	for i, msg := range tests {
		var buf bytes.Buffer
		if err := WriteTo(&buf, msg, &c); err != nil {
			t.Errorf("WriteTo(%T) (test #%d) failed: %v", msg, i, err)
			continue
		}
		reader := iobuf.NewReader(pool, &buf)
		read, err := ReadFrom(reader, &c)
		reader.Close()
		if err != nil {
			t.Errorf("ReadFrom failed (test #%d): %v", i, err)
			continue
		}
		if !reflect.DeepEqual(msg, read) {
			t.Errorf("Test #%d: Got %T = %+v, want %T = %+v", i, read, read, msg, msg)
		}
	}
}

func TestData(t *testing.T) {
	tests := []struct {
		Header  Data
		Payload string
	}{
		{Data{VCI: 10, Flow: 3}, "abcd"},
		{Data{VCI: 10, Flow: 3, flags: 1}, "batman"},
	}

	var c testControlCipher
	pool := iobuf.NewPool(0)
	allocator := iobuf.NewAllocator(pool, HeaderSizeBytes+testMACSize)
	for i, test := range tests {
		var buf bytes.Buffer
		msgW := test.Header
		msgW.Payload = allocator.Copy([]byte(test.Payload))
		if err := WriteTo(&buf, &msgW, &c); err != nil {
			t.Errorf("WriteTo(%v) failed: %v", i, err)
			continue
		}
		reader := iobuf.NewReader(pool, &buf)
		read, err := ReadFrom(reader, &c)
		if err != nil {
			t.Errorf("ReadFrom(%v) failed: %v", i, err)
			continue
		}
		msgR := read.(*Data)
		// Must compare Payload and the rest of the message separately.
		// reflect.DeepEqual(msgR, &msgW) will not cut it because the
		// iobuf.Slice objects might not pass the DeepEqual test.  That
		// is fine, the important thing is for iobuf.Slice.Content to
		// match.
		if g, w := string(msgR.Payload.Contents), test.Payload; g != w {
			t.Errorf("Mismatched payloads in test #%d. Got %q want %q", i, g, w)
		}
		msgR.Release()
		if !reflect.DeepEqual(&test.Header, msgR) {
			t.Errorf("Mismatched headers in test #%d. Got %+v want %+v", i, msgR, &test.Header)
		}
	}
}

func TestDataNoPayload(t *testing.T) {
	tests := []Data{
		{VCI: 10, Flow: 3},
		{VCI: 11, Flow: 4, flags: 10},
	}
	var c testControlCipher
	pool := iobuf.NewPool(0)
	for _, test := range tests {
		var buf bytes.Buffer
		if err := WriteTo(&buf, &test, &c); err != nil {
			t.Errorf("WriteTo(%v) failed: %v", test, err)
			continue
		}
		read, err := ReadFrom(iobuf.NewReader(pool, &buf), &c)
		if err != nil {
			t.Errorf("ReadFrom(%v) failed: %v", test, err)
			continue
		}
		msgR := read.(*Data)
		if msgR.PayloadSize() != 0 {
			t.Errorf("ReadFrom(WriteTo(%v)) returned payload of %d bytes", test, msgR.PayloadSize())
			continue
		}
		msgR.Payload = nil
		if !reflect.DeepEqual(&test, msgR) {
			t.Errorf("Wrote %v, Read %v", test, read)
		}
	}
}
