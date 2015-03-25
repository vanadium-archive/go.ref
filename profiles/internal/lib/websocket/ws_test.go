// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websocket_test

import (
	"net"
	"sync"
	"testing"

	"v.io/v23/rpc"

	"v.io/x/ref/profiles/internal/lib/websocket"
)

func packetTester(t *testing.T, dialer rpc.DialerFunc, listener rpc.ListenerFunc, txProtocol, rxProtocol string) {
	ln, err := listener(rxProtocol, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer ln.Close()
	if got, want := ln.Addr().Network(), rxProtocol; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	packetRunner(t, ln, dialer, txProtocol, ln.Addr().String())
	packetRunner(t, ln, dialer, txProtocol, ln.Addr().String())
}

func byteTester(t *testing.T, dialer rpc.DialerFunc, listener rpc.ListenerFunc, txProtocol, rxProtocol string) {
	ln, err := listener(rxProtocol, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer ln.Close()
	if got, want := ln.Addr().Network(), rxProtocol; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	byteRunner(t, ln, dialer, txProtocol, ln.Addr().String())
	byteRunner(t, ln, dialer, txProtocol, ln.Addr().String())

}

func TestWSToWS(t *testing.T) {
	byteTester(t, websocket.Dial, websocket.Listener, "ws", "ws")
	packetTester(t, websocket.Dial, websocket.Listener, "ws", "ws")
}

func TestWSToWSH(t *testing.T) {
	byteTester(t, websocket.Dial, websocket.HybridListener, "ws", "wsh")
	//packetTester(t, websocket.Dial, websocket.HybridListener, "ws", "wsh")
}

func TestWSHToWSH(t *testing.T) {
	byteTester(t, websocket.HybridDial, websocket.HybridListener, "wsh", "wsh")
	packetTester(t, websocket.HybridDial, websocket.HybridListener, "wsh", "wsh")
}

func TestTCPToWSH(t *testing.T) {
	byteTester(t, net.DialTimeout, websocket.HybridListener, "tcp", "wsh")
	packetTester(t, net.DialTimeout, websocket.HybridListener, "tcp", "wsh")
}

func TestMixed(t *testing.T) {
	ln, err := websocket.HybridListener("wsh", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer ln.Close()

	var pwg sync.WaitGroup
	packetTest := func(dialer rpc.DialerFunc, protocol string) {
		packetRunner(t, ln, dialer, protocol, ln.Addr().String())
		pwg.Done()
	}

	pwg.Add(4)
	go packetTest(websocket.Dial, "ws")
	go packetTest(net.DialTimeout, "tcp")
	go packetTest(websocket.Dial, "ws")
	go packetTest(websocket.HybridDial, "wsh")
	pwg.Wait()

	var bwg sync.WaitGroup
	byteTest := func(dialer rpc.DialerFunc, protocol string) {
		byteRunner(t, ln, dialer, protocol, ln.Addr().String())
		bwg.Done()
	}
	bwg.Add(4)
	go byteTest(websocket.Dial, "ws")
	go byteTest(net.DialTimeout, "tcp")
	go byteTest(websocket.Dial, "ws")
	go byteTest(websocket.HybridDial, "wsh")

	bwg.Wait()
}
