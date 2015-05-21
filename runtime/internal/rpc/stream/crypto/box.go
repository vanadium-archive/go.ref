// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/nacl/box"

	"v.io/v23/verror"
	"v.io/x/ref/runtime/internal/lib/iobuf"
	"v.io/x/ref/runtime/internal/rpc/stream"
)

const pkgPath = "v.io/x/ref/runtime/internal/rpc/stream/crypto"

func reg(id, msg string) verror.IDAction {
	return verror.Register(verror.ID(pkgPath+id), verror.NoRetry, msg)
}

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errCipherTextTooShort     = reg(".errCipherTextTooShort", "ciphertext too short")
	errRemotePublicKey        = reg(".errRemotePublicKey", "failed to get remote public key")
	errMessageAuthFailed      = reg(".errMessageAuthFailed", "message authentication failed")
	errUnrecognizedCipherText = reg(".errUnrecognizedCipherText", "CipherSuite {3} is not recognized. Must use one that uses Diffie-Hellman as the key exchange algorithm")
)

type boxcrypter struct {
	alloc                 *iobuf.Allocator
	sharedKey             [32]byte
	sortedPubkeys         []byte
	writeNonce, readNonce uint64
}

type BoxKey [32]byte

// BoxKeyExchanger is used to communicate public keys between peers.
type BoxKeyExchanger func(myPublicKey *BoxKey) (theirPublicKey *BoxKey, err error)

// GenerateBoxKey generates a new public/private key pair for BoxCrypter.
func GenerateBoxKey() (publicKey, privateKey *BoxKey, err error) {
	pk, sk, err := box.GenerateKey(rand.Reader)
	return (*BoxKey)(pk), (*BoxKey)(sk), err
}

// NewBoxCrypter uses Curve25519, XSalsa20 and Poly1305 to encrypt and
// authenticate messages (as defined in http://nacl.cr.yp.to/box.html).
// An ephemeral Diffie-Hellman key exchange is performed per invocation
// of NewBoxCrypter; the data sent has forward security with connection
// granularity. One round-trip is required before any data can be sent.
// BoxCrypter does NOT do anything to verify the identity of the peer.
func NewBoxCrypter(exchange BoxKeyExchanger, alloc *iobuf.Allocator) (Crypter, error) {
	pk, sk, err := GenerateBoxKey()
	if err != nil {
		return nil, err
	}
	theirPK, err := exchange(pk)
	if err != nil {
		return nil, err
	}
	if theirPK == nil {
		return nil, verror.New(errRemotePublicKey, nil)
	}
	return NewBoxCrypterWithKey(pk, sk, theirPK, alloc), nil
}

// NewBoxCrypterWithKey is used when public keys have been already exchanged between peers.
func NewBoxCrypterWithKey(myPublicKey, myPrivateKey, theirPublicKey *BoxKey, alloc *iobuf.Allocator) Crypter {
	c := boxcrypter{alloc: alloc}
	box.Precompute(&c.sharedKey, (*[32]byte)(theirPublicKey), (*[32]byte)(myPrivateKey))
	// Distinct messages between the same {sender, receiver} set are required
	// to have distinct nonces. The server with the lexicographically smaller
	// public key will be sending messages with 0, 2, 4... and the other will
	// be using 1, 3, 5...
	if bytes.Compare(myPublicKey[:], theirPublicKey[:]) < 0 {
		c.writeNonce, c.readNonce = 0, 1
		c.sortedPubkeys = append(myPublicKey[:], theirPublicKey[:]...)
	} else {
		c.writeNonce, c.readNonce = 1, 0
		c.sortedPubkeys = append(theirPublicKey[:], myPublicKey[:]...)
	}
	return &c
}

func (c *boxcrypter) Encrypt(src *iobuf.Slice) (*iobuf.Slice, error) {
	var nonce [24]byte
	binary.LittleEndian.PutUint64(nonce[:], c.writeNonce)
	c.writeNonce += 2
	ret := c.alloc.Alloc(uint(len(src.Contents) + box.Overhead))
	ret.Contents = box.SealAfterPrecomputation(ret.Contents[:0], src.Contents, &nonce, &c.sharedKey)
	src.Release()
	return ret, nil
}

func (c *boxcrypter) Decrypt(src *iobuf.Slice) (*iobuf.Slice, error) {
	var nonce [24]byte
	binary.LittleEndian.PutUint64(nonce[:], c.readNonce)
	c.readNonce += 2
	retLen := len(src.Contents) - box.Overhead
	if retLen < 0 {
		src.Release()
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errCipherTextTooShort, nil))
	}
	ret := c.alloc.Alloc(uint(retLen))
	var ok bool
	ret.Contents, ok = box.OpenAfterPrecomputation(ret.Contents[:0], src.Contents, &nonce, &c.sharedKey)
	if !ok {
		src.Release()
		ret.Release()
		return nil, verror.New(stream.ErrSecurity, nil, verror.New(errMessageAuthFailed, nil))
	}
	src.Release()
	return ret, nil
}

func (c *boxcrypter) ChannelBinding() []byte {
	return c.sortedPubkeys
}

func (c *boxcrypter) String() string {
	return fmt.Sprintf("%#v", *c)
}
