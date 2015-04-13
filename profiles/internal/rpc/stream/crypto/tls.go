// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build go1.4

package crypto

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"v.io/v23/verror"

	"v.io/x/ref/profiles/internal/lib/iobuf"
	"v.io/x/ref/profiles/internal/rpc/stream"
)

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errDeadlinesNotSupported = reg(".errDeadlinesNotSupported", "deadlines not supported")
	errEndOfEncryptedSlice   = reg(".errEndOfEncryptedSlice", "end of encrypted slice")
)

// TLSClientSessionCacheOpt specifies the ClientSessionCache used to resume TLS sessions.
// It adapts tls.ClientSessionCache to the v.io/v23/x/ref/profiles/internal/rpc/stream.VCOpt interface.
type TLSClientSessionCache struct{ tls.ClientSessionCache }

func (TLSClientSessionCache) RPCStreamVCOpt() {}

// NewTLSClientSessionCache creates a new session cache.
// TODO(ashankar): Remove this once go1.4 is released and tlsfork can be release, at that
// point use crypto/tls.NewLRUClientSessionCache directly.
func NewTLSClientSessionCache() TLSClientSessionCache {
	return TLSClientSessionCache{tls.NewLRUClientSessionCache(-1)}
}

// NewTLSClient returns a Crypter implementation that uses TLS, assuming
// handshaker was initiated by a client.
func NewTLSClient(handshaker io.ReadWriteCloser, local, remote net.Addr, sessionCache TLSClientSessionCache, pool *iobuf.Pool) (Crypter, error) {
	var config tls.Config
	// TLS + resumption + channel bindings is broken: <https://secure-resumption.com/#channelbindings>.
	config.SessionTicketsDisabled = true
	config.InsecureSkipVerify = true
	config.ClientSessionCache = sessionCache.ClientSessionCache
	return newTLSCrypter(handshaker, local, remote, &config, pool, false)
}

// NewTLSServer returns a Crypter implementation that uses TLS, assuming
// handshaker was accepted by a server.
func NewTLSServer(handshaker io.ReadWriteCloser, local, remote net.Addr, pool *iobuf.Pool) (Crypter, error) {
	return newTLSCrypter(handshaker, local, remote, ServerTLSConfig(), pool, true)
}

type fakeConn struct {
	handshakeConn io.ReadWriteCloser
	out           bytes.Buffer
	in            []byte
	laddr, raddr  net.Addr
}

func (c *fakeConn) Read(b []byte) (n int, err error) {
	if c.handshakeConn != nil {
		return c.handshakeConn.Read(b)
	}
	if len(c.in) == 0 {
		return 0, stream.NewNetError(verror.New(stream.ErrNetwork, nil, verror.New(errEndOfEncryptedSlice, nil)), false, true)
	}
	n = copy(b, c.in)
	c.in = c.in[n:]
	return
}

func (c *fakeConn) Write(b []byte) (int, error) {
	if c.handshakeConn != nil {
		return c.handshakeConn.Write(b)
	}
	return c.out.Write(b)
}

func (*fakeConn) Close() error           { return nil }
func (c *fakeConn) LocalAddr() net.Addr  { return c.laddr }
func (c *fakeConn) RemoteAddr() net.Addr { return c.raddr }
func (*fakeConn) SetDeadline(t time.Time) error {
	return verror.New(stream.ErrBadState, nil, verror.New(errDeadlinesNotSupported, nil))
}
func (*fakeConn) SetReadDeadline(t time.Time) error {
	return verror.New(stream.ErrBadState, nil, verror.New(errDeadlinesNotSupported, nil))
}
func (*fakeConn) SetWriteDeadline(t time.Time) error {
	return verror.New(stream.ErrBadState, nil, verror.New(errDeadlinesNotSupported, nil))
}

// tlsCrypter implements the Crypter interface using crypto/tls.
//
// crypto/tls provides a net.Conn, while the Crypter interface operates on
// iobuf.Slice objects. In order to adapt to the Crypter in stream.ErrNetwork, verrorterface, the
// strategy is as follows:
//
// - netTLSCrypter wraps a net.Conn with an alternative implementation
//   (fakeConn) for the TLS handshake protocol.
// - Once the TLS handshake is complete, fakeConn switches to a mode where all
//   Write calls add to a bytes.Buffer and all Read calls read from a
//   bytes.Buffer.
// - Encrypt uses tls.Conn.Write, which in-turn invokes fakeConn.Write and then
//   it extracts the contents of the underlying bytes.Buffer.
// - Decrypt adds to the read buffer and then invokes tls.Conn.Read, which
//   in-turn invokes fakeConn.Read, which reads from that buffer.
type tlsCrypter struct {
	mu    sync.Mutex
	alloc *iobuf.Allocator
	tls   *tls.Conn
	fc    *fakeConn
}

func newTLSCrypter(handshaker io.ReadWriteCloser, local, remote net.Addr, config *tls.Config, pool *iobuf.Pool, server bool) (Crypter, error) {
	fc := &fakeConn{handshakeConn: handshaker, laddr: local, raddr: remote}
	var t *tls.Conn
	if server {
		t = tls.Server(fc, config)
	} else {
		// The TLS handshake protocol ends with a message received by the client.
		// handshaker should be closed only after the handshake protocol completes.
		// So, the client closes the handshaker.
		defer handshaker.Close()
		t = tls.Client(fc, config)
	}
	if err := t.Handshake(); err != nil {
		return nil, err
	}
	// Must have used Diffie-Hellman to exchange keys (so that the key selection
	// is independent of any TLS certificates used). This helps ensure that
	// identities exchanged during the vanadium authentication protocol cannot be
	// stolen and are bound to the session key established during the TLS handshake.
	switch cs := t.ConnectionState().CipherSuite; cs {
	case tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA:
	case tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA:
	case tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA:
	case tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA:
	case tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA:
	case tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA:
	case tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:
	case tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:
	case tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:
	default:
		t.Close()
		return nil, verror.New(stream.ErrBadArg, nil, verror.New(errUnrecognizedCipherText, nil, fmt.Sprintf("0x%04x", cs)))
	}
	fc.handshakeConn = nil
	return &tlsCrypter{
		alloc: iobuf.NewAllocator(pool, 0),
		tls:   t,
		fc:    fc,
	}, nil
}

func (c *tlsCrypter) Encrypt(plaintext *iobuf.Slice) (*iobuf.Slice, error) {
	defer plaintext.Release()
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.fc.out.Reset()
	if _, err := c.tls.Write(plaintext.Contents); err != nil {
		return nil, err
	}
	return c.alloc.Copy(c.fc.out.Bytes()), nil
}

func (c *tlsCrypter) Decrypt(ciphertext *iobuf.Slice) (*iobuf.Slice, error) {
	defer ciphertext.Release()
	if ciphertext.Size() == 0 {
		return ciphertext, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fc.in = ciphertext.Contents
	// Given the cipher suites used, len(plaintext) < len(ciphertext)
	// (ciphertext includes TLS record headers). Allocating space for
	// plaintext based on ciphertext.Size should suffice.
	plaintext := c.alloc.Alloc(uint(ciphertext.Size()))
	out := plaintext.Contents
	for {
		n, err := c.tls.Read(out)
		if err != nil {
			if _, exit := err.(*stream.NetError); exit {
				break
			}
			plaintext.Release()
			return nil, err
		}
		out = out[n:]
	}
	plaintext.Contents = plaintext.Contents[:plaintext.Size()-len(out)]
	return plaintext, nil
}

func (c *tlsCrypter) String() string {
	state := c.tls.ConnectionState()
	return fmt.Sprintf("TLS CipherSuite:0x%04x Resumed:%v", state.CipherSuite, state.DidResume)
}

// ServerTLSConfig returns the tls.Config used by NewTLSServer.
func ServerTLSConfig() *tls.Config {
	c, err := tls.X509KeyPair([]byte(serverCert), []byte(serverKey))
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		// TLS + resumption + channel bindings is broken: <https://secure-resumption.com/#channelbindings>.
		SessionTicketsDisabled: true,
		Certificates:           []tls.Certificate{c},
		InsecureSkipVerify:     true,
		// RC4_128_SHA is 4-5X faster compared to the other cipher suites.
		// There are concerns with its security (see http://en.wikipedia.org/wiki/RC4 and
		// https://www.usenix.org/conference/usenixsecurity13/technical-sessions/paper/alFardan),
		// so this decision will be revisted.
		// TODO(ashankar,ataly): Figure out what cipher to use and how to
		// have a speedy Go implementation of it.
		CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA},
	}
}

func (c *tlsCrypter) ChannelBinding() []byte {
	return c.tls.ConnectionState().TLSUnique
}

// TODO(ashankar): Get rid of TLS certificates completely after implementing an
// anonymous key-exchange mechanism. See F.1.1.1 in RFC 5246.
//
// PEM-encoded certificates and keys used in the tests.
// One way to generate them is:
//   go run $GOROOT/src/pkg/crypto/tls/generate_cert.go  --host=localhost --duration=87600h --ecdsa-curve=P256
// (This generates a self-signed certificate valid for 10 years)
// (The --ecdsa-curve flag has not yet been submitted back to the Go repository)
// which will create cert.pem and key.pem files.
const (
	serverCert = `
-----BEGIN CERTIFICATE-----
MIIBbTCCAROgAwIBAgIQMD+Kzawjvhij1B/BmvHxLDAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE0MDcxODIzMTYxMloXDTI0MDcxNTIzMTYxMlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABLiz
Ajsly1DS8NJF2KE195V83TgidfgGEB7nudscdKWH3+5uQHgCc+2BV/7AGGj3yePR
ZZLzYD95goJ/a7eet/2jSzBJMA4GA1UdDwEB/wQEAwIAoDATBgNVHSUEDDAKBggr
BgEFBQcDATAMBgNVHRMBAf8EAjAAMBQGA1UdEQQNMAuCCWxvY2FsaG9zdDAKBggq
hkjOPQQDAgNIADBFAiAb4tBxggEpnKdxv66TBVFxAUn3EBWX25XlL1G2GF8RkAIh
AOAwys3mvzM4Td/2kV9QNyQPZ9kLLQr9A9ryB0H3N9Yz
-----END CERTIFICATE-----
`
	serverKey = `
-----BEGIN ECDSA PRIVATE KEY-----
MHcCAQEEIPLfwg+SVC2/xUcKq0bI9y2+SDEEdCeGuxuBz22BhAw1oAoGCCqGSM49
AwEHoUQDQgAEuLMCOyXLUNLw0kXYoTX3lXzdOCJ1+AYQHue52xx0pYff7m5AeAJz
7YFX/sAYaPfJ49FlkvNgP3mCgn9rt563/Q==
-----END ECDSA PRIVATE KEY-----
`
)
