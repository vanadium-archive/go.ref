// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
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

func testMessages(t *testing.T, ctx *context.T, cases []message) {
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
	ctx, shutdown := v23.Init()
	defer shutdown()
	ep1, err := v23.NewEndpoint(
		"@5@tcp@foo.com:1234@00112233445566778899aabbccddeeff@m@v.io/foo")
	if err != nil {
		t.Fatal(err)
	}
	ep2, err := v23.NewEndpoint(
		"@5@tcp@bar.com:1234@00112233445566778899aabbccddeeff@m@v.io/bar")
	if err != nil {
		t.Fatal(err)
	}
	testMessages(t, ctx, []message{
		&setup{versions: version.RPCVersionRange{Min: 3, Max: 5}},
		&setup{
			versions: version.RPCVersionRange{Min: 3, Max: 5},
			peerNaClPublicKey: &[32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13,
				14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
			peerRemoteEndpoint: ep1,
			peerLocalEndpoint:  ep2,
		},
		&setup{},
	})
}

func TestTearDown(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	testMessages(t, ctx, []message{
		&tearDown{Message: "foobar"},
		&tearDown{},
	})
}

func TestAuth(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	p := v23.GetPrincipal(ctx)
	sig, err := p.Sign([]byte("message"))
	if err != nil {
		t.Fatal(err)
	}
	testMessages(t, ctx, []message{
		&auth{bkey: 1, dkey: 5, channelBinding: sig, publicKey: p.PublicKey()},
		&auth{bkey: 1, dkey: 5, channelBinding: sig},
		&auth{channelBinding: sig, publicKey: p.PublicKey()},
		&auth{},
	})
}

func TestOpenFlow(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	testMessages(t, ctx, []message{
		&openFlow{id: 23, initialCounters: 1 << 20, bkey: 42, dkey: 55},
		&openFlow{},
	})
}

func TestAddReceiveBuffers(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	testMessages(t, ctx, []message{
		&release{},
		&release{counters: map[flowID]uint64{
			4: 233,
			9: 423242,
		}},
	})
}

func TestData(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	testMessages(t, ctx, []message{
		&data{id: 1123, flags: 232, payload: [][]byte{[]byte("fake payload")}},
		&data{},
	})
}

func TestUnencryptedData(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()
	testMessages(t, ctx, []message{
		&unencryptedData{id: 1123, flags: 232, payload: [][]byte{[]byte("fake payload")}},
		&unencryptedData{},
	})
}
