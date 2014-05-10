package crypto

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"veyron/runtimes/google/lib/iobuf"
)

var errDeadlinesNotSupported = errors.New("deadlines not supported")

// TLSClientSessionCacheOpt specifies the ClientSessionCache used to resume TLS sessions.
// It adapts tls.ClientSessionCache to the veyron2/ipc/stream.VCOpt interface.
type TLSClientSessionCache struct{ tls.ClientSessionCache }

func (TLSClientSessionCache) IPCStreamVCOpt() {}

// NewTLSClient returns a Crypter implementation that uses TLS, assuming
// handshaker was initiated by a client.
func NewTLSClient(handshaker net.Conn, sessionCache tls.ClientSessionCache, pool *iobuf.Pool) (Crypter, error) {
	var config tls.Config
	config.InsecureSkipVerify = true
	config.ClientSessionCache = sessionCache
	return newTLSCrypter(handshaker, &config, pool, false)
}

// NewTLSServer returns a Crypter implementation that uses TLS, assuming
// handshaker was accepted by a server.
func NewTLSServer(handshaker net.Conn, pool *iobuf.Pool) (Crypter, error) {
	return newTLSCrypter(handshaker, ServerTLSConfig(), pool, true)
}

type fakeConn struct {
	handshakeConn net.Conn
	out           bytes.Buffer
	in            []byte
	laddr, raddr  net.Addr
}

func (c *fakeConn) Read(b []byte) (n int, err error) {
	if c.handshakeConn != nil {
		return c.handshakeConn.Read(b)
	}
	if len(c.in) == 0 {
		return 0, tempError{}
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

func (*fakeConn) Close() error                       { return nil }
func (c *fakeConn) LocalAddr() net.Addr              { return c.laddr }
func (c *fakeConn) RemoteAddr() net.Addr             { return c.raddr }
func (*fakeConn) SetDeadline(t time.Time) error      { return errDeadlinesNotSupported }
func (*fakeConn) SetReadDeadline(t time.Time) error  { return errDeadlinesNotSupported }
func (*fakeConn) SetWriteDeadline(t time.Time) error { return errDeadlinesNotSupported }

// tempError implements net.Error and returns true for Temporary.
// Providing this error in fakeConn.Read allows tls.Conn.Read to return with an
// error without changing underlying state.
type tempError struct{}

func (tempError) Error() string   { return "end of encrypted slice" }
func (tempError) Timeout() bool   { return false }
func (tempError) Temporary() bool { return true }

// tlsCrypter implements the Crypter interface using crypto/tls.
//
// crypto/tls provides a net.Conn, while the Crypter interface operates on
// iobuf.Slice objects. In order to adapt to the Crypter interface, the
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

func newTLSCrypter(handshaker net.Conn, config *tls.Config, pool *iobuf.Pool, server bool) (Crypter, error) {
	fc := &fakeConn{handshakeConn: handshaker, laddr: handshaker.LocalAddr(), raddr: handshaker.RemoteAddr()}
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
	// Must have used Diffie-Hellman to exchange keys (so that the key
	// selection is independent of any TLS certificates used).
	// This helps ensure that identities exchanged during the veyron
	// authentication protocol
	// (http://goto.google.com/veyron:authentication) cannot be stolen and
	// are bound to the session key established during the TLS handshake.
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
		return nil, fmt.Errorf("CipherSuite 0x%04x is not recognized. Must use one that uses Diffie-Hellman as the key exchange algorithm", cs)
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
			if _, exit := err.(tempError); exit {
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
		Certificates:       []tls.Certificate{c},
		InsecureSkipVerify: true,
		ClientAuth:         tls.NoClientCert,
		// TLS_ECDHE_RSA_WITH_RC4_128_SHA is 4-5X faster compared to
		// the other cipher suites and is what google.com seems to use.
		CipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA},
	}
}

// TODO(ashankar): Get rid of TLS certificates completely after implementing an
// anonymous key-exchange mechanism. See F.1.1.1 in RFC 5246.
//
// PEM-encoded certificates and keys used in the tests.
// One way to generate them is:
//   go run $GOROOT/src/pkg/crypto/tls/generate_cert.go  --host=localhost
// which will create cert.pem and key.pem files.
const (
	serverCert = `
-----BEGIN CERTIFICATE-----
MIIC1jCCAj+gAwIBAgIJAOsQamnsz2kWMA0GCSqGSIb3DQEBBQUAMIGDMQswCQYD
VQQGEwJERTEMMAoGA1UECAwDTlJXMQ4wDAYDVQQHDAVFYXJ0aDEXMBUGA1UECgwO
UmFuZG9tIENvbXBhbnkxCzAJBgNVBAsMAklUMRcwFQYDVQQDDA53d3cucmFuZG9t
LmNvbTEXMBUGCSqGSIb3DQEJARYIZ2F1dGhhbXQwHhcNMTMwNDIzMjEzMTA4WhcN
MjMwNDIxMjEzMTA4WjCBgzELMAkGA1UEBhMCREUxDDAKBgNVBAgMA05SVzEOMAwG
A1UEBwwFRWFydGgxFzAVBgNVBAoMDlJhbmRvbSBDb21wYW55MQswCQYDVQQLDAJJ
VDEXMBUGA1UEAwwOd3d3LnJhbmRvbS5jb20xFzAVBgkqhkiG9w0BCQEWCGdhdXRo
YW10MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDBaDlmU0csZctqYP8AWJ60
IYGPmT/gGWGo6p0B6jPy02LuY91jQAn0XkiAdjgdtkkkWQyRtQgaaGsGC6qT5qVX
Ogx/5l/wb5hOa75gGiOdaGxStkzCjS8hAn4Lr0AbI/JmssUQ0xwNJr6t+aHBJ5Go
gjG0TsedkLL3qw6ktQd47wIDAQABo1AwTjAdBgNVHQ4EFgQUh166SbXiiSTt+Tud
rLWaA0sS3bQwHwYDVR0jBBgwFoAUh166SbXiiSTt+TudrLWaA0sS3bQwDAYDVR0T
BAUwAwEB/zANBgkqhkiG9w0BAQUFAAOBgQB8550EwYMrcFXEwQHktpFrcaOEUWN+
50NeS0lJ0IHwb31dcMywCX0xsteKyUwkXUCSjE8Ubnktjelo3KPMaur78Jy12pK1
g3Ay6y3nBDKwpBDPcoy7Pt/pz0yL8Qy54fVnU2iQBiHMjTR/kmDsK+BwRksJfk9V
MFLsr6ZAZxOPbg==
-----END CERTIFICATE-----
`
	serverKey = `
-----BEGIN PRIVATE KEY-----
MIICdgIBADANBgkqhkiG9w0BAQEFAASCAmAwggJcAgEAAoGBAMFoOWZTRyxly2pg
/wBYnrQhgY+ZP+AZYajqnQHqM/LTYu5j3WNACfReSIB2OB22SSRZDJG1CBpoawYL
qpPmpVc6DH/mX/BvmE5rvmAaI51obFK2TMKNLyECfguvQBsj8mayxRDTHA0mvq35
ocEnkaiCMbROx52QsverDqS1B3jvAgMBAAECgYEAl2Xk+Orb3i9ZSs7fDwBQS6Wm
7CgEzoJP5pCxk1woij9bRE28cgMhR7++dYEVcHzPSLrEkhLqYvG2RadAQkLczcy+
NgXFm1I0HcZXbVT2rafaKS27GpT7NicIrIw48goncMwAI0+UB3Ply9RDwfs+VhDo
G2a8JTVx2FNpJoIIJOECQQDl80AJPi17TbJehEByQOF0Q7KgfN4aD9hx+E6SLdPq
ddn0xqnmsbBD1EPv25qeAtQ6sHRxjlP03gvhQ4CQQ0+nAkEA11ExtkqGXayf2hAe
dMwi2JrAuIGtOCQHQOCAADYgIH+3/SIf05kk/PUiXFTlGkm69qpBmLPaiDSfHV6g
taT1eQJAe9KClveOUilCdTbN5TgerxaNJ3JVvr7tlGFbHcfjpwsS9IXNk1X3Tm8M
rioYliF72qaN7V/wwZiX2RMaNZSpXQJAXmuBlEG8CGoBsztsT6WRBlFef8qF7l+G
OsH3/5+8mOPJCB0lvcGjgbXxenHUAaIhdbeVimQcSaxhthxf9ye+aQJAMstlAS7X
4rJXYVJUL5JQISgz/D5BzM5pbgJivVRcHO2Qk3HZO2F95Sg3lpD1tdOWBtOhOyRS
AS91NC8w9ruJeg==
-----END PRIVATE KEY-----
`
)
