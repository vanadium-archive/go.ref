package message

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron/runtimes/google/ipc/version"
	"veyron.io/veyron/veyron/runtimes/google/lib/iobuf"
	"veyron.io/veyron/veyron2/naming"
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
		&OpenVC{VCI: 2,
			DstEndpoint: version.Endpoint("tcp", "batman.com:1990", naming.FixedRoutingID(0xba7)),
			SrcEndpoint: version.Endpoint("tcp", "google.com:80", naming.FixedRoutingID(0xba6)),
		},
		&OpenVC{
			VCI:         4,
			DstEndpoint: version.Endpoint("tcp", "batman.com:1990", naming.FixedRoutingID(0xba7)),
			SrcEndpoint: version.Endpoint("tcp", "google.com:80", naming.FixedRoutingID(0xba6)),
			Counters:    counters,
		},

		&CloseVC{VCI: 1},
		&CloseVC{VCI: 2, Error: "some error"},

		&AddReceiveBuffers{},
		&AddReceiveBuffers{Counters: counters},

		&OpenFlow{VCI: 1, Flow: 10, InitialCounters: 1 << 24},

		&HopSetup{
			Versions: version.Range{Min: 21, Max: 71},
			Options: []HopSetupOption{
				&NaclBox{PublicKey: [32]byte{'h', 'e', 'l', 'l', 'o', 'w', 'o', 'r', 'l', 'd'}},
				&NaclBox{PublicKey: [32]byte{7, 67, 31}},
			},
		},

		&HopSetupStream{Data: []byte("HelloWorld")},
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
