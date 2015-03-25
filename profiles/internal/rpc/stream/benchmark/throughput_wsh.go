// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import (
	"io"
	"net"
	"testing"

	"v.io/x/ref/profiles/internal/lib/websocket"
)

// benchmarkWS sets up nConns WS connections and measures throughput.
func benchmarkWSH(b *testing.B, protocol string, nConns int) {
	rchan := make(chan net.Conn, nConns)
	wchan := make(chan net.Conn, nConns)
	ln, err := websocket.HybridListener("wsh", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("websocket.HybridListener failed: %v", err)
		return
	}
	defer ln.Close()
	// One goroutine to dial nConns connections.
	go func() {
		for i := 0; i < nConns; i++ {
			var conn net.Conn
			var err error
			switch protocol {
			case "tcp":
				conn, err = net.Dial("tcp", ln.Addr().String())
			case "ws":
				conn, err = websocket.Dial("ws", ln.Addr().String(), 0)
			}
			if err != nil {
				b.Fatalf("Dial(%q, %q) failed: %v", protocol, ln.Addr(), err)
				wchan <- nil
				return
			}
			if protocol == "tcp" {
				// Write a dummy byte since wsh waits for magic byte forever.
				conn.Write([]byte("."))
			}
			wchan <- conn
		}
		close(wchan)
	}()
	// One goroutine to accept nConns connections.
	go func() {
		for i := 0; i < nConns; i++ {
			conn, err := ln.Accept()
			if err != nil {
				b.Fatalf("Accept failed: %v", err)
				rchan <- nil
				return
			}
			if protocol == "tcp" {
				// Read a dummy byte.
				conn.Read(make([]byte, 1))
			}
			rchan <- conn
		}
		close(rchan)
	}()

	var readers []io.ReadCloser
	var writers []io.WriteCloser
	for r := range rchan {
		readers = append(readers, r)
	}
	for w := range wchan {
		writers = append(writers, w)
	}
	if b.Failed() {
		return
	}
	(&throughputTester{b: b, readers: readers, writers: writers}).Run()
}
