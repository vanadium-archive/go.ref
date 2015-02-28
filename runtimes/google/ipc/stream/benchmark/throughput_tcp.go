package benchmark

import (
	"io"
	"net"
	"testing"
)

// benchmarkTCP sets up nConns TCP connections and measures throughput.
func benchmarkTCP(b *testing.B, nConns int) {
	rchan := make(chan net.Conn, nConns)
	wchan := make(chan net.Conn, nConns)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("net.Listen failed: %v", err)
		return
	}
	defer ln.Close()
	// One goroutine to dial nConns connections.
	go func() {
		for i := 0; i < nConns; i++ {
			conn, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				b.Fatalf("net.Dial(%q, %q) failed: %v", "tcp", ln.Addr(), err)
				wchan <- nil
				return
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
