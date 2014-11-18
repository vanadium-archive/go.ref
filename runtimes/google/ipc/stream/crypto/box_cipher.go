package crypto

import (
	"encoding/binary"
	"errors"

	"code.google.com/p/go.crypto/nacl/box"
	"code.google.com/p/go.crypto/salsa20/salsa"
)

// cbox implements a ControlCipher using go.crypto/nacl/box.
type cbox struct {
	sharedKey [32]byte
	enc       cboxStream
	dec       cboxStream
}

// cboxStream implements one stream of encryption or decryption.
type cboxStream struct {
	counter uint64
	nonce   [24]byte
	// buffer is a temporary used for in-place crypto.
	buffer []byte
}

const (
	cboxMACSize = box.Overhead
)

var (
	errMessageTooShort = errors.New("control cipher: message is too short")
)

func (s *cboxStream) alloc(n int) []byte {
	if len(s.buffer) < n {
		s.buffer = make([]byte, n*2)
	}
	return s.buffer[:0]
}

func (s *cboxStream) currentNonce() *[24]byte {
	return &s.nonce
}

func (s *cboxStream) advanceNonce() {
	s.counter++
	binary.LittleEndian.PutUint64(s.nonce[:], s.counter)
}

// setupXSalsa20 produces a sub-key and Salsa20 counter given a nonce and key.
//
// See, "Extending the Salsa20 nonce," by Daniel J. Bernsten, Department of
// Computer Science, University of Illinois at Chicago, 2008.
func setupXSalsa20(subKey *[32]byte, counter *[16]byte, nonce *[24]byte, key *[32]byte) {
	// We use XSalsa20 for encryption so first we need to generate a
	// key and nonce with HSalsa20.
	var hNonce [16]byte
	copy(hNonce[:], nonce[:])
	salsa.HSalsa20(subKey, &hNonce, key, &salsa.Sigma)

	// The final 8 bytes of the original nonce form the new nonce.
	copy(counter[:], nonce[16:])
}

// NewControlCipher returns a ControlCipher for IPC V6.
func NewControlCipherIPC6(peersPublicKey, privateKey *[32]byte, isServer bool) ControlCipher {
	var c cbox
	box.Precompute(&c.sharedKey, peersPublicKey, privateKey)
	// The stream is full-duplex, and we want the directions to use different
	// nonces, so we set bit (1 << 64) in the server-to-client stream, and leave
	// it cleared in the client-to-server stream.  advanceNone touches only the
	// first 8 bytes, so this change is permanent for the duration of the
	// stream.
	if isServer {
		c.enc.nonce[8] = 1
	} else {
		c.dec.nonce[8] = 1
	}
	return &c
}

// MACSize implements the ControlCipher method.
func (c *cbox) MACSize() int {
	return cboxMACSize
}

// Seal implements the ControlCipher method.
func (c *cbox) Seal(data []byte) error {
	n := len(data)
	if n < cboxMACSize {
		return errMessageTooShort
	}
	tmp := c.enc.alloc(n)
	nonce := c.enc.currentNonce()
	out := box.SealAfterPrecomputation(tmp, data[:n-cboxMACSize], nonce, &c.sharedKey)
	c.enc.advanceNonce()
	copy(data, out)
	return nil
}

// Open implements the ControlCipher method.
func (c *cbox) Open(data []byte) bool {
	n := len(data)
	if n < cboxMACSize {
		return false
	}
	tmp := c.dec.alloc(n - cboxMACSize)
	nonce := c.dec.currentNonce()
	out, ok := box.OpenAfterPrecomputation(tmp, data, nonce, &c.sharedKey)
	if !ok {
		return false
	}
	c.dec.advanceNonce()
	copy(data, out)
	return true
}

// Encrypt implements the ControlCipher method.
func (c *cbox) Encrypt(data []byte) {
	var subKey [32]byte
	var counter [16]byte
	nonce := c.enc.currentNonce()
	setupXSalsa20(&subKey, &counter, nonce, &c.sharedKey)
	c.enc.advanceNonce()
	salsa.XORKeyStream(data, data, &counter, &subKey)
}

// Decrypt implements the ControlCipher method.
func (c *cbox) Decrypt(data []byte) {
	var subKey [32]byte
	var counter [16]byte
	nonce := c.dec.currentNonce()
	setupXSalsa20(&subKey, &counter, nonce, &c.sharedKey)
	c.dec.advanceNonce()
	salsa.XORKeyStream(data, data, &counter, &subKey)
}
