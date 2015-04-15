// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !nacl

package websocket

import (
	"net"
	"testing"
	"time"
)

func TestAcceptsAreNotSerialized(t *testing.T) {
	ln, err := HybridListener("wsh", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { go ln.Close() }()
	portscan := make(chan struct{})

	// Goroutine that continuously accepts connections.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	// Imagine some client was port scanning and thus opened a TCP
	// connection (but never sent the bytes)
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Error(err)
		}
		close(portscan)
		// Keep the connection alive by blocking on a read.  (The read
		// should return once the test exits).
		var buf [1024]byte
		conn.Read(buf[:])
	}()
	// Another client that dials a legitimate connection should not be
	// blocked on the portscanner.
	// (Wait for the portscanner to establish the TCP connection first).
	<-portscan
	conn, err := Dial(ln.Addr().Network(), ln.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}
