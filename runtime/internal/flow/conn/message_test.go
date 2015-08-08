// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"errors"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc/version"
	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/test"
)

func init() {
	test.Init()
}

func TestVarInt(t *testing.T) {
	cases := []uint64{
		0x00, 0x01,
		0x7f, 0x80,
		0xff, 0x100,
		0xffff, 0x10000,
		0xffffff, 0x1000000,
		0xffffffff, 0x100000000,
		0xffffffffff, 0x10000000000,
		0xffffffffffff, 0x1000000000000,
		0xffffffffffffff, 0x100000000000000,
		0xffffffffffffffff,
	}
	ctx, cancel := context.RootContext()
	defer cancel()
	for _, want := range cases {
		got, b, valid := readVarUint64(ctx, writeVarUint64(want, []byte{}))
		if !valid {
			t.Fatalf("error reading %x", want)
		}
		if len(b) != 0 {
			t.Errorf("unexpected buffer remaining for %x: %v", want, b)
		}
		if got != want {
			t.Errorf("got: %d want: %d", got, want)
		}
	}
}

func testMessages(t *testing.T, cases []message) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	w, r, _ := newMRWPair(ctx)
	wp, rp := newMessagePipe(w), newMessagePipe(r)
	for _, want := range cases {
		ch := make(chan struct{})
		go func() {
			if err := wp.writeMsg(ctx, want); err != nil {
				t.Errorf("unexpected error for %#v: %v", want, err)
			}
			close(ch)
		}()
		got, err := rp.readMsg(ctx)
		if err != nil {
			t.Errorf("unexpected error reading %#v: %v", want, err)
		}
		<-ch
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got: %#v, want %#v", got, want)
		}
	}
}

func TestSetup(t *testing.T) {
	testMessages(t, []message{
		&setup{versions: version.RPCVersionRange{Min: 3, Max: 5}},
		&setup{},
	})
}

func TestTearDown(t *testing.T) {
	testMessages(t, []message{
		&tearDown{Err: errors.New("foobar")},
		&tearDown{},
	})
}

func TestOpenFlow(t *testing.T) {
	testMessages(t, []message{
		&openFlow{id: 23, initialCounters: 1 << 20},
		&openFlow{},
	})
}

func TestAddReceiveBuffers(t *testing.T) {
	testMessages(t, []message{
		&addRecieveBuffers{},
		&addRecieveBuffers{counters: map[flowID]uint64{
			4: 233,
			9: 423242,
		}},
	})
}

func TestData(t *testing.T) {
	testMessages(t, []message{
		&data{id: 1123, flags: 232, payload: [][]byte{[]byte("fake payload")}},
		&data{},
	})
}

func TestUnencryptedData(t *testing.T) {
	testMessages(t, []message{
		&unencryptedData{id: 1123, flags: 232, payload: [][]byte{[]byte("fake payload")}},
		&unencryptedData{},
	})
}
