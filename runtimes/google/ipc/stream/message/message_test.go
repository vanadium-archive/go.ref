package message

import (
	"bytes"
	"reflect"
	"testing"

	"veyron/runtimes/google/ipc/version"
	"veyron/runtimes/google/lib/iobuf"
	"veyron2/naming"
)

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
	}

	pool := iobuf.NewPool(0)
	for i, msg := range tests {
		var buf bytes.Buffer
		if err := WriteTo(&buf, msg); err != nil {
			t.Errorf("WriteTo(%T) (test #%d) failed: %v", msg, i, err)
			continue
		}
		reader := iobuf.NewReader(pool, &buf)
		read, err := ReadFrom(reader)
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

	pool := iobuf.NewPool(0)
	allocator := iobuf.NewAllocator(pool, HeaderSizeBytes)
	for i, test := range tests {
		var buf bytes.Buffer
		msgW := test.Header
		msgW.Payload = allocator.Copy([]byte(test.Payload))
		if err := WriteTo(&buf, &msgW); err != nil {
			t.Errorf("WriteTo(%v) failed: %v", i, err)
			continue
		}
		reader := iobuf.NewReader(pool, &buf)
		read, err := ReadFrom(reader)
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
	pool := iobuf.NewPool(0)
	for _, test := range tests {
		var buf bytes.Buffer
		if err := WriteTo(&buf, &test); err != nil {
			t.Errorf("WriteTo(%v) failed: %v", test, err)
			continue
		}
		read, err := ReadFrom(iobuf.NewReader(pool, &buf))
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
