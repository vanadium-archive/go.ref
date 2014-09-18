// +build !go1.4

// TODO(ashankar): Remove the build tag and replace the tls import with crypto/tls
// when go1.4 is released.

package benchmark

import (
	"io"
	"net"
	"testing"

	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/crypto"
	tls "veyron.io/veyron/veyron/runtimes/google/ipc/stream/crypto/tlsfork"
)

func benchmarkTLS(b *testing.B, nConns int) {
	rchan := make(chan *tls.Conn, nConns)
	wchan := make(chan *tls.Conn, nConns)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("net.Listen failed: %v", err)
		return
	}

	defer ln.Close()
	// One goroutine to dial nConns connections.
	var tlsConfig tls.Config
	tlsConfig.InsecureSkipVerify = true
	go func() {
		for i := 0; i < nConns; i++ {
			conn, err := tls.Dial("tcp", ln.Addr().String(), &tlsConfig)
			if err != nil {
				b.Fatalf("tls.Dial(%q, %q) failed: %v", "tcp", ln.Addr(), err)
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
			}
			server := tls.Server(conn, crypto.ServerTLSConfig())
			server.Handshake()
			rchan <- server
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
