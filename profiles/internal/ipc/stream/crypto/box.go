package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"golang.org/x/crypto/nacl/box"

	"v.io/x/ref/profiles/internal/lib/iobuf"
)

type boxcrypter struct {
	conn                  net.Conn
	alloc                 *iobuf.Allocator
	sharedKey             [32]byte
	sortedPubkeys         []byte
	writeNonce, readNonce uint64
}

// NewBoxCrypter uses Curve25519, XSalsa20 and Poly1305 to encrypt and
// authenticate messages (as defined in http://nacl.cr.yp.to/box.html).
// An ephemeral Diffie-Hellman key exchange is performed per invocation
// of NewBoxCrypter; the data sent has forward security with connection
// granularity. One round-trip is required before any data can be sent.
// BoxCrypter does NOT do anything to verify the identity of the peer.
func NewBoxCrypter(conn net.Conn, pool *iobuf.Pool) (Crypter, error) {
	pk, sk, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	var theirPK [32]byte
	errChan := make(chan error)
	defer close(errChan)
	go func() { _, err := conn.Write(pk[:]); errChan <- err }()
	go func() { _, err := io.ReadFull(conn, theirPK[:]); errChan <- err }()
	if err := <-errChan; err != nil {
		return nil, err
	}
	if err := <-errChan; err != nil {
		return nil, err
	}
	ret := &boxcrypter{conn: conn, alloc: iobuf.NewAllocator(pool, 0)}
	box.Precompute(&ret.sharedKey, &theirPK, sk)
	// Distinct messages between the same {sender, receiver} set are required
	// to have distinct nonces. The server with the lexicographically smaller
	// public key will be sending messages with 0, 2, 4... and the other will
	// be using 1, 3, 5...
	if bytes.Compare(pk[:], theirPK[:]) < 0 {
		ret.writeNonce, ret.readNonce = 0, 1
		ret.sortedPubkeys = append(pk[:], theirPK[:]...)
	} else {
		ret.writeNonce, ret.readNonce = 1, 0
		ret.sortedPubkeys = append(theirPK[:], pk[:]...)
	}
	return ret, nil
}

func (c *boxcrypter) Encrypt(src *iobuf.Slice) (*iobuf.Slice, error) {
	defer src.Release()
	var nonce [24]byte
	binary.LittleEndian.PutUint64(nonce[:], c.writeNonce)
	c.writeNonce += 2
	ret := c.alloc.Alloc(uint(len(src.Contents) + box.Overhead))
	ret.Contents = box.SealAfterPrecomputation(ret.Contents[:0], src.Contents, &nonce, &c.sharedKey)
	return ret, nil
}

func (c *boxcrypter) Decrypt(src *iobuf.Slice) (*iobuf.Slice, error) {
	defer src.Release()
	var nonce [24]byte
	binary.LittleEndian.PutUint64(nonce[:], c.readNonce)
	c.readNonce += 2
	retLen := len(src.Contents) - box.Overhead
	if retLen < 0 {
		return nil, fmt.Errorf("ciphertext too short")
	}
	ret := c.alloc.Alloc(uint(retLen))
	var ok bool
	ret.Contents, ok = box.OpenAfterPrecomputation(ret.Contents[:0], src.Contents, &nonce, &c.sharedKey)
	if !ok {
		return nil, fmt.Errorf("message authentication failed")
	}
	return ret, nil
}

func (c *boxcrypter) ChannelBinding() []byte {
	return c.sortedPubkeys
}

func (c *boxcrypter) String() string {
	return fmt.Sprintf("%#v", *c)
}
